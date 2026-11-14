package agent

// run_in_flight_test.go — the regression guard for Agent.RunInFlight, i.e. for
// the question "is there a run RIGHT NOW that InterruptRun could abandon?".
//
// THE DEFECT THIS FILE NOW REFUSES (task 01a1b4a2). `runScopedRoot` published
// `a.runCtx` at the start of a run and NOTHING cleared it when the run RETURNED:
// the caller's `defer cancel()` cancels the DERIVED context, so the published
// handle kept `Err() == nil` forever. Between two turns of a long-lived server
// that meant:
//
//   - `RunInFlight()` answered true with no run in existence, and
//   - `InterruptRun()` returned true — latching `runInterrupted` on a context
//     that had already finished — so `POST /api/chat/interrupt` acked an
//     interruption of a turn nobody could be running.
//
// The fix is `endRunScope`, deferred on every return path of a run (runEngine,
// Chat, ChatOps) with the scope THAT run published, so an overlapping later run
// keeps its handle. This file pins both halves:
//
//  1. the probe DESCRIBES the run it is asked about — true while the run is
//     live, and an interrupt aimed at it really ends it (the web badge's "this
//     turn can be stopped" reading, which is the half `turnInFlight &&
//     agentRunInterruptible` must keep);
//  2. a run that has RETURNED is not reported as a run — `RunInFlight()` and
//     `InterruptRun()` both answer false BETWEEN turns — and the next turn is
//     still interruptible, so the retirement cannot be a one-way latch.

import (
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// runInFlightTextServer answers every request with one short text stream: the
// shortest honest TURN — it starts, streams, ends, and publishes/retires a run
// scope in between, which is all this file needs to observe.
func runInFlightTextServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"ok"}, "end_turn")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newRunInFlightAgent is the minimal chat-shaped agent: no run cap, no session
// log, so a turn is the model round and nothing else.
func newRunInFlightAgent(srv *httptest.Server) *Agent {
	return &Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()},
		Ctx:     &Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
}

// TestRunInFlightIsFalseBeforeAnyRun pins the trivially-correct half: a fresh
// agent has no run, so the probe reports nothing to interrupt.
func TestRunInFlightIsFalseBeforeAnyRun(t *testing.T) {
	a := &Agent{Logger: log.New(io.Discard, "", 0)}
	if a.RunInFlight() {
		t.Error("RunInFlight() = true on a fresh agent; nothing has run yet")
	}
}

// TestRunInFlightDescribesTheTurnItEnds is the REPLACEMENT for the old
// "SurvivesTheTurnItDescribes" arm, which pinned the defect as a FACT. The
// intent that arm existed for is kept and is now asserted positively: while a
// turn is in flight the probe says so (so a badge offers a stop that works), and
// an interrupt aimed at it really ends the turn — the badge and the button
// cannot disagree.
func TestRunInFlightDescribesTheTurnItEnds(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	// A stream that HOLDS: the turn is genuinely in flight while the assertions
	// below run, which is the state the probe must describe.
	var (
		mu       sync.Mutex
		streamed int32
	)
	srv := interruptStub(t, &streamed, &mu)
	t.Cleanup(srv.Close)

	a := newRunInFlightAgent(srv)
	done := make(chan error, 1)
	go func() {
		_, err := a.Chat("answer at length", nil)
		done <- err
	}()

	if !waitForRunScopePublished(t, a) {
		t.Fatal("Chat never published its run scope — there would be nothing to interrupt")
	}
	if !a.RunInFlight() {
		t.Fatal("RunInFlight() = false while a turn is in flight; the badge would hide a stop that works")
	}
	// The read half and the act agree: what the probe promised, the interrupt
	// delivers. The interrupt's OWN report is asserted below (the between-turns
	// arm), so this one only needs the turn to actually end.
	_ = a.InterruptRun()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the interrupted turn never returned — the probe claimed a stoppable run it could not stop")
	}
}

// TestRunInFlightIsFalseBetweenTurns is the defect's own regression arm: after
// the turn RETURNS, there is no run, and the interrupt route must not claim one.
// It also proves the retirement is not a one-way latch — the next turn can still
// be interrupted.
func TestRunInFlightIsFalseBetweenTurns(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	a := newRunInFlightAgent(runInFlightTextServer(t))
	if _, err := a.Chat("say ok", nil); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	// AFTER the turn returned. Asserted synchronously (not with a wait): the
	// retirement is a defer on the run's own return path, so by the time the
	// call returns the handle MUST be gone.
	if a.RunInFlight() {
		t.Error("RunInFlight() = true after its turn returned — the probe is answering about a run that no longer exists")
	}
	if a.InterruptRun() {
		t.Error("InterruptRun() between turns returned true — the ack would claim an interruption of a finished turn, " +
			"and it latches runInterrupted on a context nobody is running")
	}
	a.runMu.Lock()
	leftover := a.runCtx
	a.runMu.Unlock()
	if leftover != nil {
		t.Errorf("the run handle is still published after the turn returned (%p) — runScopedRoot was not paired with endRunScope", leftover)
	}

	// NOT a one-way latch: the NEXT turn publishes its own handle and can be
	// abandoned, which is what makes the retirement safe rather than a
	// convenient lie.
	var (
		mu       sync.Mutex
		streamed int32
	)
	hold := interruptStub(t, &streamed, &mu)
	t.Cleanup(hold.Close)
	b := newRunInFlightAgent(hold)
	done := make(chan error, 1)
	go func() {
		_, err := b.Chat("answer at length", nil)
		done <- err
	}()
	if !waitForRunScopePublished(t, b) {
		t.Fatal("the turn AFTER an idle probe never published a run scope — the retirement leaked")
	}
	if !b.InterruptRun() {
		t.Fatal("the run after a completed turn could not be abandoned")
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the second turn was not ended by its interrupt")
	}
	if b.RunInFlight() {
		t.Error("RunInFlight() = true after the second turn returned")
	}
}
