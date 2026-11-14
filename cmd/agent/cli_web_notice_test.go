package main

// cli_web_notice_test.go — the regression guard for the WEB carrier's session
// inbox (cli_web_notice.go).
//
// WHAT IT PINS, and why each half is needed. A `gopi notify --session <id>
// --message <text>` is written by ANOTHER process, so it raises no in-process
// edge; before this carrier had a wait point the delivery sat in the file for
// ever, because the only loader was a round boundary and a web turn only ever
// started from a page POST. Three facts therefore have to hold together and
// none of them implies the others:
//
//  1. the message is CONSUMED — the inbox file gains its `delivered` marker,
//     which is what makes "the model saw it" durable across processes;
//  2. it becomes a MODEL CALL — the mock endpoint receives a request whose body
//     carries the delivered text, i.e. the turn really ran (a marker alone
//     could be written by a loader nobody consumed);
//  3. a live subscriber of the standing stream RECEIVES the `notice` frame —
//     the delivery must be visible to a page that never typed anything, which
//     is the whole point of the carrier.
//
// RED PROOF: every surface this file touches already exists before the change
// except the route itself, so running it against the pre-change tree fails on
// fact 3's very first step (GET /api/events → 404) rather than on a compile
// error — the honest red, measured with `git stash` on the two production
// files.

import (
	"bufio"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

// webNoticeTestText is the delivered sentence. It is deliberately unlikely to
// appear in any prompt or tool schema, so "the mock saw it" cannot be satisfied
// by the model's own framing.
const webNoticeTestText = "probe-from-another-process-9f2"

// webNoticeTestAnswer is the mock model's answer for the notice turn.
const webNoticeTestAnswer = "the notice turn ran"

// webNoticeBodyRecorder is a mock model endpoint that records every request
// body it is handed (so a test can ask WHICH text reached the wire, not merely
// whether a call happened).
type webNoticeBodyRecorder struct {
	mu     sync.Mutex
	bodies []string
}

func (r *webNoticeBodyRecorder) record(body string) {
	r.mu.Lock()
	r.bodies = append(r.bodies, body)
	r.mu.Unlock()
}

func (r *webNoticeBodyRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.bodies...)
}

// sawText reports whether any recorded request body carried text.
func (r *webNoticeBodyRecorder) sawText(text string) bool {
	for _, b := range r.snapshot() {
		if strings.Contains(b, text) {
			return true
		}
	}
	return false
}

// newWebNoticeRecorderServer starts the mock endpoint: it records the request
// body and answers with one streaming text response (the same SSE shape the
// other web tests use, so the turn streams exactly as it does in production).
func newWebNoticeRecorderServer(t *testing.T) (*httptest.Server, *webNoticeBodyRecorder) {
	t.Helper()
	rec := &webNoticeBodyRecorder{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("mock: expected POST, got %s", r.Method)
		}
		body, _ := io.ReadAll(r.Body) //stylegate:ignore a recording mock: a read failure leaves an empty body, which the assertion then reports
		rec.record(string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, webNoticeTestAnswer)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

// newWebNoticeTestServer builds the SAME single persistent agent `gopi web`
// creates (a real session logger, so the inbox path resolves), a webServer
// around it, and the bare handler the server serves.
func newWebNoticeTestServer(t *testing.T) (*webServer, *agent.Agent, *httptest.Server, *httptest.Server, *webNoticeBodyRecorder) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	// The queue and the wake channel are PROCESS-wide; a leftover notice from
	// another test would be peeked by this turn and poison the assertion.
	t.Cleanup(func() { tools.TakeJobNotices() })

	mockSrv, rec := newWebNoticeRecorderServer(t)

	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	rl, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	// The loggers' Close errors are discarded on purpose: the run's assertions do
	// not depend on the JSONL handle, and a close failure at cleanup time has
	// nowhere left to be reported (the test is already finishing).
	t.Cleanup(func() {
		sl.Close() //stylegate:ignore the session logger's close has no assertion riding on it
		rl.Close() //stylegate:ignore the raw logger's close has no assertion riding on it
	})

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		SessionLog: sl,
		RawLog:     rl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true

	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}
	httpSrv := httptest.NewServer(ws.webChatHandler())
	t.Cleanup(httpSrv.Close)
	return ws, ag, mockSrv, httpSrv, rec
}

// inboxRecords reads the session's inbox as raw lines (the file format is the
// contract; a helper that decoded it would hide a marker written in the wrong
// shape).
func inboxRecords(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(tools.SessionInboxPath(dir))
	if err != nil {
		t.Fatalf("read inbox %s: %v", tools.SessionInboxPath(dir), err)
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// TestWebInboxNoticeTurnConsumesAndStreams is the epic's minimal reproduction:
// a delivery made by another process becomes (a) a consumed inbox record,
// (b) a model call carrying the delivered text, and (c) a `notice` frame on a
// standing /api/events connection — with the page never POSTing anything.
func TestWebInboxNoticeTurnConsumesAndStreams(t *testing.T) {
	ws, ag, _, httpSrv, rec := newWebNoticeTestServer(t)

	// The inbox lives in the session RECORD's own container — resolved by the
	// layout package the loader uses, never by cutting a path string here.
	dir := sessionlayout.RecordContainer(ag.SessionLog.Path())
	sessionID := ag.SessionLog.SessionID()

	// THE DELIVERY, through the SAME writer `gopi notify --session … ` uses —
	// so this test cannot drift from the CLI verb's file shape (Rule 14).
	if _, err := tools.SessionInboxDeliver(dir, sessionID, webNoticeTestText, inputsource.Notify); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}

	// Subscribe to the standing stream BEFORE the turn can run, exactly as a
	// page does (EventSource: one long GET). The response headers arrive at
	// once; the frames arrive as the turn produces them.
	res, err := http.Get(httpSrv.URL + webEventsPath)
	if err != nil {
		t.Fatalf("GET %s: %v", webEventsPath, err)
	}
	if res.StatusCode != http.StatusOK {
		res.Body.Close() //stylegate:ignore the fatal below is the report; a close error adds nothing
		t.Fatalf("GET %s status = %d, want 200 (the standing stream is not registered)", webEventsPath, res.StatusCode)
	}
	defer res.Body.Close()
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("Content-Type = %q, want text/event-stream", ct)
	}

	// Start the carrier's ONE goroutine. The delivery already exists, so its
	// first `SessionInboxIdleLoad` makes the level true before the cadence
	// matters — that is the durable case (a message delivered while nothing was
	// running at all), and it keeps the test off the 2 s tick.
	ws.startInboxNoticeLoop()
	defer ws.stopInboxNoticeLoop()

	// FACT 3: the subscriber receives the notice frame — read frames until the
	// deadline, since the turn runs on the server's own goroutine.
	notice := readEventFrame(t, res.Body, webEventNotice, webNoticeTestFrameTimeout)
	var payload map[string]string
	if err := json.Unmarshal([]byte(notice), &payload); err != nil {
		t.Fatalf("notice frame payload %q is not JSON: %v", notice, err)
	}
	if !strings.Contains(payload["text"], webNoticeTestText) {
		t.Errorf("notice frame text = %q, want it to carry %q", payload["text"], webNoticeTestText)
	}

	// FACT 2: the delivered text reached the MODEL, i.e. the turn really ran.
	waitFor(t, webNoticeTestFrameTimeout, "the model to receive the delivered text", func() bool {
		return rec.sawText(webNoticeTestText)
	})

	// FACT 1: the inbox carries the delivered marker for that message, which is
	// what makes "the model saw it" true across processes.
	waitFor(t, webNoticeTestFrameTimeout, "the inbox to record kind=delivered", func() bool {
		lines := inboxRecords(t, dir)
		sawMessage := false
		for _, line := range lines {
			if strings.Contains(line, `"kind":"message"`) && strings.Contains(line, webNoticeTestText) {
				sawMessage = true
			}
			if strings.Contains(line, `"kind":"delivered"`) {
				return sawMessage
			}
		}
		return false
	})

	if n := ws.ensureNoticeHub().droppedFrames(); n != 0 {
		t.Errorf("hub dropped %d frame(s) with one live subscriber, want 0", n)
	}
}

// TestWebInboxNoticeTurnDoesNotInterruptAnInFlightTurn pins the busy case: with
// s.mu HELD (a turn in flight) the wake must mint NOTHING — the notice stays in
// the queue, nothing is consumed, and the in-flight turn's own boundary is the
// one that will inject it.
func TestWebInboxNoticeTurnDoesNotInterruptAnInFlightTurn(t *testing.T) {
	ws, ag, _, _, rec := newWebNoticeTestServer(t)

	dir := sessionlayout.RecordContainer(ag.SessionLog.Path())
	if _, err := tools.SessionInboxDeliver(dir, ag.SessionLog.SessionID(), webNoticeTestText, inputsource.Notify); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}
	// The load is the agent's own merge; doing it here puts a notice in the
	// queue WITHOUT going through the carrier, so the assertion below is about
	// the carrier's decision and not about whether a file was read.
	if !ag.SessionInboxIdleLoad() {
		t.Fatalf("SessionInboxIdleLoad reported no pending notice after a delivery")
	}
	// Drain the wake token that load raised, so the turn below cannot be blamed
	// on a token left over from the setup.
	select {
	case <-agent.SessionInboxIdleWake():
	default:
	}

	ws.mu.Lock()           // a turn is in flight for the whole of this test
	tools.TakeJobNotices() // the in-flight turn's own boundary drained it
	ws.mu.Unlock()

	// Nothing is owed now, so the wake must produce no turn at all — the
	// simplest, fully deterministic half of "never interrupt".
	ws.runInboxNoticeTurn()
	if rec.sawText(webNoticeTestText) {
		t.Error("a turn ran while s.mu was held: the carrier interrupted a turn in flight")
	}
}

// readEventFrame reads SSE frames from body until one named `event` arrives,
// returning its data line. It fails the test at the deadline instead of hanging
// (an unanswered stream is the failure this whole epic is about).
func readEventFrame(t *testing.T, body io.Reader, want string, timeout time.Duration) string {
	t.Helper()
	type result struct{ data string }
	ch := make(chan result, 1)
	go func() {
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		current := ""
		var data strings.Builder
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event:"):
				current = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
			case strings.HasPrefix(line, "data:"):
				data.WriteString(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
			case line == "":
				if current == want {
					ch <- result{data: data.String()}
					return
				}
				current = ""
				data.Reset()
			}
		}
	}()
	select {
	case r := <-ch:
		return r.data
	case <-time.After(timeout):
		t.Fatalf("no %q frame arrived on %s within %v", want, webEventsPath, timeout)
		return ""
	}
}

// waitFor polls cond until it holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out after %v waiting for %s", timeout, what)
		}
		time.Sleep(webNoticeTestPollInterval) //stylegate:allow-sleep bounded poll for a side effect on the server's own goroutine
	}
}

// The test's two timing numbers (Rule 1).
const (
	// webNoticeTestFrameTimeout bounds how long the test waits for a frame or a
	// side effect that happens on the server's own goroutine. It is generous on
	// purpose: a slow CI box must not turn a correct carrier into a flake.
	webNoticeTestFrameTimeout = 15 * time.Second
	// webNoticeTestPollInterval is how often the assertions re-check.
	webNoticeTestPollInterval = 20 * time.Millisecond
)

// TestWebInboxNoticeHubFansOutAndReleases pins the hub's own contract, which
// the epic's carrier depends on: every live subscriber gets every frame, a
// released subscriber stops receiving, and release is idempotent (a handler that
// defers it and also calls it cannot double-close a channel).
func TestWebInboxNoticeHubFansOutAndReleases(t *testing.T) {
	h := newWebNoticeHub()
	a, releaseA := h.subscribe()
	b, releaseB := h.subscribe()

	h.publish(webEventNotice, []byte(`{"text":"x"}`))
	for name, ch := range map[string]<-chan webNoticeFrame{"a": a, "b": b} {
		select {
		case f := <-ch:
			if f.event != webEventNotice {
				t.Errorf("subscriber %s got event %q, want %q", name, f.event, webEventNotice)
			}
		default:
			t.Fatalf("subscriber %s received no frame", name)
		}
	}

	releaseA()
	releaseB()
	releaseB() // idempotent by contract
	if n := h.subscriberCount(); n != 0 {
		t.Errorf("subscriberCount after release = %d, want 0", n)
	}
	// A closed channel is observable: a frame published with no subscribers is
	// dropped and counted, never sent to a released one.
	h.publish(webEventNotice, []byte(`{"text":"y"}`))
	if n := h.droppedFrames(); n != 0 {
		t.Errorf("droppedFrames with no subscribers = %d, want 0", n)
	}
	if _, open := <-a; open {
		t.Error("a released subscriber's channel is still open")
	}
}

// TestWebInboxNoticeConstantsAgreeWithThePage is the Rule 1b pair check: the
// server's route and event names and the page's must be the SAME strings, or a
// page would connect to a route nobody serves (or render a frame nobody sends).
func TestWebInboxNoticeConstantsAgreeWithThePage(t *testing.T) {
	pairs := []struct {
		name         string
		server, page string
	}{
		{"events path", webEventsPath, pageConstant(t, "API_EVENTS")},
		{"notice event", webEventNotice, pageConstant(t, "EV_NOTICE")},
		// The operator-facing warning (context pressure). A separate frame name on
		// both sides: the page must open no answer bubble for it.
		{"warning event", webEventWarning, pageConstant(t, "EV_WARNING")},
	}
	for _, p := range pairs {
		if p.server != p.page {
			t.Errorf("%s: server spells %q, the page spells %q", p.name, p.server, p.page)
		}
	}
	if !strings.Contains(webChatPage, "connectEvents()") {
		t.Error("the page never calls connectEvents(): a server-driven turn has no reader")
	}
}

// pageConstant extracts a JS `const NAME='value'` from the embedded page, so the
// wire vocabulary is compared as VALUES rather than by finding the name.
func pageConstant(t *testing.T, name string) string {
	t.Helper()
	marker := "const " + name + "='"
	i := strings.Index(webChatPage, marker)
	if i < 0 {
		t.Fatalf("the page does not declare %s", name)
	}
	rest := webChatPage[i+len(marker):]
	j := strings.Index(rest, "'")
	if j < 0 {
		t.Fatalf("the page's %s declaration is unterminated", name)
	}
	return rest[:j]
}
