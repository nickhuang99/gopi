package main

// Shutdown-resource regressions for epic 01a16dab — the two leaks the epic's
// rig measured on a real `gopi web` server, one per scenario the epic asks to be
// treated separately:
//
//	1. SIGTERM WHILE A TURN IS IN FLIGHT. The finalizer closes the session under
//	   s.mu and a turn holds s.mu for its whole model round-trip, so a peer that
//	   has not answered yet kept a SIGTERMed server alive indefinitely; the
//	   operator's supervisor SIGKILLs it, and a SIGKILLed process runs no Go
//	   code — no session_end, an open CHATS row, and a tool round's bash child
//	   left behind. Measured: still alive 30s after SIGTERM, exit -9,
//	   session_end=0, last record signal_verdict. After the fix: exit 0 in
//	   8.6ms with session_end=1, and a tool round's `sleep 300` child reaped.
//
//	2. BROWSER DISCONNECT MID-TURN. Nothing watched the request context, so a
//	   closed tab still cost the server the ENTIRE model round while holding the
//	   session's turn lock — measured: the next turn waited 1.94s behind it.
//	   After the fix: 33ms.
//
// They are separate tests because the epics's own point is that the two events
// have different meanings — a shutdown cancels the turn AND closes the session,
// a disconnect cancels the turn and the server keeps serving.

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/config"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/heavytest"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/testutil"
)

// --- 1. SIGTERM mid-turn -------------------------------------------------

// TestWebSignalShutdownAbandonsInFlightTurn drives the REAL binary: one turn
// whose scripted provider holds the stream open forever, then SIGTERM. Before
// the fix the server never exited (the finalizer waited on the turn lock while
// the turn waited on the held round); now the turn is abandoned, the lock is
// released through the normal path, and the ONE session_end is written.
func TestWebSignalShutdownAbandonsInFlightTurn(t *testing.T) {
	// The provider holds the round open until it is released, so the SIGTERM
	// below lands while the turn is genuinely mid-round-trip.
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: webSignalTurnReplyText, Hold: true})
	defer llm.Close()
	home := t.TempDir()
	env := testutil.MockEnv(t, home, llm.URL())

	p := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"}, "", env, "")
	base := p.WebBaseURL()
	sessRoot := filepath.Join(home, ".gopi", config.SessionsDirName)
	p.WaitFor("the sandboxed session JSONL", func() bool { return testutil.SessionRecordPath(sessRoot) != "" })

	// Drive the turn from a goroutine: it cannot finish, by construction.
	go func() {
		client := &http.Client{Timeout: webSignalTurnTimeout}
		resp, err := client.Post(base+"/api/chat", "application/json", strings.NewReader(`{"message":"hold this round"}`))
		if err == nil {
			io.Copy(io.Discard, resp.Body) //stylegate:ignore the body is drained only to let the handler finish; its content is asserted nowhere
			resp.Body.Close()              //stylegate:ignore same: the drained body's close error carries no state
		}
	}()

	// Readiness of the TURN, not of the listener: the mock reports the held
	// request, so SIGTERM cannot arrive before the round is really in flight
	// (a signal that lands too early would pass this test vacuously).
	deadline := time.Now().Add(webSignalTurnTimeout)
	for llm.HeldCount() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("the scripted provider never saw a held round within %s\nserver stderr:\n%s", webSignalTurnTimeout, p.Stderr())
		}
		time.Sleep(webShutdownPollInterval) //stylegate:allow-sleep bounded poll for the mock to report the held round; no event fires when the request lands
	}

	// THE MEASUREMENT: SIGTERM with a turn in flight must still be a CLEAN exit
	// inside the server's own graceful window — not a kill by a supervisor.
	p.Signal(syscall.SIGTERM)
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("gopi web exit after a mid-turn SIGTERM = %d, want 0 (a non-zero code here means the server was killed, not shut down)\nstderr:\n%s", code, p.Stderr())
		}
	case <-time.After(webShutdownWaitBudget):
		t.Fatalf("gopi web did not exit within %s after a mid-turn SIGTERM — the in-flight turn was not abandoned, so the finalizer is still waiting on the session lock\nstderr:\n%s", webShutdownWaitBudget, p.Stderr())
	}

	// The session is closed exactly once, and the abandonment is visible in the
	// log: a silent close would hide which of the two paths ran.
	if !strings.Contains(p.Stderr(), "abandoning the in-flight turn") {
		t.Errorf("server log does not report abandoning the in-flight turn — the fix must be OBSERVABLE, not merely effective:\n%s", p.Stderr())
	}
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if got := sess.Counts()[sessionevent.EventTypeSessionEnd]; got != 1 {
		t.Errorf("%s count after a mid-turn SIGTERM = %d, want exactly 1 — %s\nstderr:\n%s",
			sessionevent.EventTypeSessionEnd, got, sess.Summary(), p.Stderr())
	}
}

// --- 2. Browser disconnect mid-turn -------------------------------------

// holdingStreamServer answers every POST with SSE headers + one text delta, then
// blocks until the REQUEST CONTEXT is cancelled (or the ceiling expires). That
// is what a peer whose answer has not arrived yet looks like: the server's model
// call produces its first fragment and then waits.
//
// The ceiling exists so a MISSING cancellation fails as a measured slow turn
// rather than a hung test (the ceiling far exceeds the assertion below).
func holdingStreamServer(t *testing.T, ceiling time.Duration) *hitsServer {
	t.Helper()
	hits := &hitsServer{}
	hits.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.n.Add(1)
		if r.Method != http.MethodPost {
			t.Errorf("mock: expected POST, got %s", r.Method)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}\n\n")
		fmt.Fprint(w, "event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\n")
		fmt.Fprint(w, "event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n")
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(ceiling):
		}
	}))
	return hits
}

// TestWebChatClientDisconnectAbandonsTurn locks the browser-disconnect half:
// when the REQUEST is cancelled (the tab closed mid-stream) the turn must stop
// waiting on the peer, instead of burning the whole round while holding the
// session's turn lock.
//
// The assertion is measured, not structural: the handler's own duration. The
// holding server blocks until the request context dies, which happens only when
// the AGENT's run context is cancelled — i.e. only if the watcher fired. Without
// the watcher the handler blocks for the ceiling.
func TestWebChatClientDisconnectAbandonsTurn(t *testing.T) {
	heavytest.Require(t, heavytest.CriterionE2ENetwork)
	t.Setenv("GOPI_HOME", t.TempDir())

	// The ceiling is far above the budget asserted below: a run that does the
	// slow thing is reported as such, never as a hang.
	const (
		peerCeiling      = 8 * time.Second
		abandonedMaxWait = 3 * time.Second
	)
	mockSrv := holdingStreamServer(t, peerCeiling)
	defer mockSrv.Close()

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: mockSrv.URL(), APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0, // the default: no per-turn budget, so only the watcher can stop this
		Logger:  log.New(io.Discard, "", 0),
	}
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	// The request context plays the browser: it is cancelled shortly after the
	// turn starts streaming, which is exactly what a closed tab produces.
	ctx, cancel := context.WithCancel(context.Background())
	body := strings.NewReader(`{"message":"stream into a tab that closes"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", body).WithContext(ctx)
	rec := httptest.NewRecorder()
	cancelAt := time.AfterFunc(webShutdownPollInterval*10, cancel)
	defer cancelAt.Stop()

	started := time.Now()
	ws.handleChat(rec, req)
	elapsed := time.Since(started)

	if elapsed > abandonedMaxWait {
		t.Errorf("handleChat took %v after the client hung up, want <= %v — the turn kept waiting on the peer instead of being abandoned (the session's turn lock is held for that whole time)",
			elapsed, abandonedMaxWait)
	}
	if !strings.Contains(rec.Body.String(), "event: done") {
		t.Errorf("the abandoned turn did not reach its terminal frame; stream=%q", shutdownCut(rec.Body.String(), 200))
	}

	// The turn was abandoned for the RIGHT reason, and only that turn: the
	// process-wide interrupt must NOT have fired, or this server would report an
	// instant failure on every later turn (interruptBase is never re-armed).
	if got := ag.TurnInterruptReason(); got != agent.InterruptReasonClientGone {
		t.Errorf("abandoned turn recorded reason %q, want %q — a reader of the session cannot tell a closed tab from a shutdown", got, agent.InterruptReasonClientGone)
	}
	// AND THE SERVER KEEPS SERVING. This is the assertion that catches the WRONG
	// fix: cancelling the process-wide interrupt (Interrupt) stops the abandoned
	// turn just as well, but interruptBase is never re-armed, so every later turn
	// would be born cancelled and report an instant failure. The second turn
	// below is answered normally by the mock, which only a run-scoped cancel can
	// allow.
	before := mockSrv.Hits()
	second := httptest.NewRecorder()
	ws.handleChat(second, httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"still serving?"}`)))
	if !strings.Contains(second.Body.String(), "event: done") {
		t.Errorf("the turn AFTER a client disconnect never reached its terminal frame — the disconnect did more than abandon ONE turn; stream=%q", shutdownCut(second.Body.String(), 200))
	}
	if mockSrv.Hits() != before+1 {
		t.Errorf("the second turn did not reach the provider (%d → %d requests); the conversation is dead after a client disconnect", before, mockSrv.Hits())
	}
}

// --- 3. The bounded finalizer -------------------------------------------

// TestFinalizeChatSessionDoesNotBlockForever locks the belt to the suspenders:
// even if a turn somehow still holds the session lock when the finalizer runs,
// the close is BOUNDED and says so instead of hanging the process. The lock is
// held past the grace window, and the finalizer must return anyway (reporting
// it) rather than block.
func TestFinalizeChatSessionDoesNotBlockForever(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())

	// holdFor is just past the grace window: long enough that only the bound can
	// end the wait, short enough that the holder goroutine finishes inside this
	// test (the suite fails a test that parks a goroutine past its own end).
	const holdFor = webShutdownGrace + 2*webLockPollInterval

	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	ag := &agent.Agent{
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	// Occupy the turn domain the way a turn does, and hold it PAST the bound.
	ws.mu.Lock()
	holderDone := make(chan struct{})
	go func() {
		defer close(holderDone)
		time.Sleep(holdFor) //stylegate:allow-sleep this sleep IS the test's subject: a lock held past the finalizer's grace bound
		ws.mu.Unlock()
	}()
	// The holder must outlive this test's assertions and then leave cleanly: the
	// wait below is what keeps the suite's goroutine sweep honest.
	defer func() { <-holderDone }()

	started := time.Now()
	ws.finalizeChatSession(true)
	elapsed := time.Since(started)

	if elapsed >= holdFor {
		t.Errorf("finalizeChatSession waited %v for a lock held for %v — it waited for the TURN, which is the unbounded wait this fix removes", elapsed, holdFor)
	}
	if elapsed < webShutdownGrace {
		t.Errorf("finalizeChatSession returned in %v, before its own grace bound of %v — the bound is not being applied", elapsed, webShutdownGrace)
	}
}

// --- helpers ------------------------------------------------------------

// webShutdownPollInterval is how often the readiness loops above re-check an
// observable fact. Named (Rule 1) so the three tests share one cadence.
const webShutdownPollInterval = 20 * time.Millisecond

// webShutdownWaitBudget bounds how long a SIGTERMed server may take to exit. It
// is deliberately ABOVE the server's own webShutdownGrace: the server's window
// is for draining HTTP, and a test that waited only that long would report its
// own impatience as a server defect.
const webShutdownWaitBudget = webShutdownGrace + 10*time.Second

// hitsServer is the holdingStreamServer handle: the httptest server plus the
// count of requests that reached it, which is how the second arm proves the
// conversation is still alive (an assertion on output alone cannot tell "the
// model answered" from "the server returned a canned line").
type hitsServer struct {
	srv *httptest.Server
	n   atomic.Int64
}

func (h *hitsServer) URL() string          { return h.srv.URL }
func (h *hitsServer) Client() *http.Client { return h.srv.Client() }
func (h *hitsServer) Close()               { h.srv.Close() }
func (h *hitsServer) Hits() int64          { return h.n.Load() }

// shutdownCut shortens a stream for an error message; a full SSE turn body is
// not readable in a failure line.
func shutdownCut(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- 4. goroutine accounting --------------------------------------------

// TestWebTurnsDoNotLeakGoroutines gives the epic's "goroutine 数量对比" a NUMBER,
// and it measures the only thing that constitutes a leak: GROWTH PER TURN.
//
// A first turn legitimately leaves goroutines behind that are NOT leaks — the
// HTTP transport's connection/dial machinery, the DNS resolver, a keepalive
// ticker — because they belong to the CLIENT POOL, not to the turn. So the
// baseline is taken AFTER one warm-up turn, when that one-time population
// already exists; from there, every additional turn (ordinary, abandoned, and
// reset) must add nothing permanent. Comparing against a cold baseline instead
// would report the pool as a leak forever (measured: 6 → 9 on the first run of
// this test, all three of them pool/ticker goroutines, no per-turn survivor).
func TestWebTurnsDoNotLeakGoroutines(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())

	llm := testutil.NewMockLLM(t,
		testutil.Resp{Text: "warm-up answer"},
		testutil.Resp{Text: "answer one"},
		testutil.Resp{Text: "answer two"},
	)
	defer llm.Close()
	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: llm.URL(), APIKey: "k", HTTPClient: &http.Client{Timeout: webShutdownWaitBudget}},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	settled := func() int {
		last := runtime.NumGoroutine()
		for i := 0; i < goroutineSettleRounds; i++ {
			time.Sleep(webShutdownPollInterval) //stylegate:allow-sleep settle-and-recheck: a goroutine still on its way out is not yet a measurement
			now := runtime.NumGoroutine()
			if now >= last {
				return now
			}
			last = now
		}
		return last
	}

	// WARM-UP: one ordinary turn, so the client pool's one-time goroutines exist
	// before the baseline is taken.
	postChat(t, ws, "warm up the client pool", nil)
	before := settled()

	// The three shapes that touch the shutdown/reset surface.
	postChat(t, ws, "first question", nil)
	postChat(t, ws, "second question", nil)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(`{"message":"abandoned turn"}`)).WithContext(ctx)
	rec := httptest.NewRecorder()
	timer := time.AfterFunc(webShutdownPollInterval, cancel)
	ws.handleChat(rec, req)
	timer.Stop()
	ws.resetConversation()

	after := settled()
	t.Logf("goroutines: baseline after warm-up=%d, after 2 turns + 1 abandoned turn + 1 reset=%d", before, after)
	if after != before {
		buf := make([]byte, goroutineStackDumpBytes)
		n := runtime.Stack(buf, true)
		t.Logf("STACKS AT THE END (a per-turn goroutine would be created by handleChat):\n%s", buf[:n])
	}
	if after > before+goroutineLeakTolerance {
		t.Errorf("goroutines grew from %d to %d over 3 turns + 1 reset (tolerance %d) — a per-turn goroutine is outliving its request",
			before, after, goroutineLeakTolerance)
	}
}

// goroutineStackDumpBytes bounds the diagnostic dump a failing run prints; the
// stack of every goroutine in this test file is well inside it, and an unbounded
// buffer would make the failure line unreadable.
const goroutineStackDumpBytes = 1 << 16

// goroutineSettleRounds bounds the settle-and-recheck loop above; a count that
// is still FALLING is not yet a measurement.
const goroutineSettleRounds = 20

// goroutineLeakTolerance is the slack the runtime's own short-lived goroutines
// need (GC workers, timer wheels). It is deliberately smaller than the number of
// turn shapes the test drives, so a genuine per-turn leak cannot hide in it.
const goroutineLeakTolerance = 2
