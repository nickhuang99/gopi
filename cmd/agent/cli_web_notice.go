package main

// cli_web_notice.go — the WEB carrier's half of the session inbox: a delivery
// made by another process (`gopi notify --session <id> --message <text>`)
// becomes a turn OF THIS SERVER, and that turn's frames reach the page over a
// standing SSE connection.
//
// THE GAP THIS CLOSES. `gopi chat` has an idle wait point (cli_chat.go's two
// parked `select`s, plus the poller that raises agent.SessionInboxIdleWake). A
// browser has no stdin to select on and `gopi web` had no wait point at all, so
// a message delivered into a web session's inbox sat in the file forever: the
// only thing that would ever load it was a round boundary, and a boundary is
// reachable only from inside a turn, and a web turn only ever started when the
// page POSTed one. Measured before this file: `chatInboxTurnText` was called
// from cli_chat.go twice and from cli_web.go never, and no cli_web.go line
// named SessionInboxIdle*.
//
// WHAT THIS FILE DOES NOT DO — the whole design. It does not parse an inbox, it
// does not add a queue and it does not add a take point. It calls the SAME
// three agent exports the REPL calls (`SessionInboxIdleLoad` / `IdlePoll` /
// `IdleWake`), peeks the SAME queue (`tools.PeekJobNotices`) and composes the
// opening line with the SAME function (`agent.ChatJobNoticeOpenLine`). The take
// stays where it has always been — `tools.TakeJobNotices`, at the turn's own
// round boundary, which injects the notice "BEFORE the request is built"
// (internal/agent/chat_ops.go). This file therefore cannot disagree with the
// REPL about what is pending or what a notice says: there is one implementation
// and two carriers (Rules 14/15).
//
// IT ALSO DOES NOT INTERRUPT A TURN. The busy case takes the SAME trade the
// console documented (cmd/agent/chat_notice_turn.go): the notice stays in the
// queue, the in-flight turn's own round boundary injects it, and nothing about
// that turn changes. Here the mechanism is one `TryLock`: if a turn holds s.mu
// this goroutine returns WITHOUT touching the agent, the notices remain
// pending, and the next tick raises the wake again — so a busy server can never
// mint a concurrent second turn on the one persistent agent (which would race
// Ctx.Messages and Cost, the exact hazard s.mu exists to serialize).

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/sessionevent"
	"github.com/nick/gopi/internal/tools"
)

// The hub's two numbers (Rule 1).
const (
	// webNoticeSubscriberBuffer is how many frames one SSE subscriber may fall
	// behind before a frame is dropped. A model turn emits one frame per delta,
	// so a browser that stops reading must not be able to block the turn that
	// is producing them — but a drop is REPORTED (webNoticeDropReport), never
	// silent (Rule 15).
	webNoticeSubscriberBuffer = 256
)

// webNoticeDropReport is printed when a subscriber's buffer is full: the page
// missed a frame, which is a fact the operator can act on (reload) and must not
// be discovered by comparing the screen against the transcript.
const webNoticeDropReport = "[gopi web] events: a subscriber fell %d frame(s) behind — the frame was dropped\n"

// webNoticeFrame is one SSE frame on its way to the subscribers: the event name
// and the already-encoded payload, i.e. exactly the two fields writeSSEFrame
// writes. Encoding happens ONCE per frame, in the turn, rather than once per
// subscriber.
type webNoticeFrame struct {
	event string
	data  []byte
}

// webNoticeHub fans one server-driven turn's frames out to every connected
// page. It is process-local state and nothing else: no file, no socket, no
// lifecycle of its own — a subscriber's channel is created when its request
// arrives and closed when it leaves (Rule 35: no resident artifact).
//
// WHY A HUB AT ALL, rather than writing the turn's frames into one request's
// writer: the turn is not started by a request. The page that must see it is
// whoever happens to be watching /api/events, which may be a tab opened before
// the delivery and may be replaced mid-turn by a reload.
type webNoticeHub struct {
	mu   sync.Mutex
	subs map[int]chan webNoticeFrame
	next int
	// dropped counts the frames no subscriber could take, so the condition is
	// visible in a test and in the report rather than only on stderr.
	dropped int
}

// newWebNoticeHub builds an empty hub. A hub with no subscribers is valid and
// the ordinary state of a `gopi web` nobody has opened yet.
func newWebNoticeHub() *webNoticeHub {
	return &webNoticeHub{subs: map[int]chan webNoticeFrame{}}
}

// subscribe registers one SSE connection. The returned release function is
// idempotent, so a handler that defers it and also calls it on an error path
// cannot double-close a channel.
func (h *webNoticeHub) subscribe() (<-chan webNoticeFrame, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	id := h.next
	h.next++
	ch := make(chan webNoticeFrame, webNoticeSubscriberBuffer)
	h.subs[id] = ch
	release := func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		if c, ok := h.subs[id]; ok {
			delete(h.subs, id)
			close(c)
		}
	}
	return ch, release
}

// publish hands one frame to every live subscriber. A subscriber whose buffer
// is full loses the frame and the loss is counted and REPORTED — the turn keeps
// running, because one browser that stopped reading must not stall a model round
// the server is already paying for.
func (h *webNoticeHub) publish(event string, data []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	frame := webNoticeFrame{event: event, data: data}
	droppedNow := 0
	for _, ch := range h.subs {
		select {
		case ch <- frame:
		default:
			droppedNow++
		}
	}
	if droppedNow > 0 {
		h.dropped += droppedNow
		fmt.Fprintf(os.Stderr, webNoticeDropReport, droppedNow)
	}
}

// droppedFrames reports how many frames no subscriber could take. Read by the
// regression test, which asserts the steady path drops none.
func (h *webNoticeHub) droppedFrames() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.dropped
}

// subscriberCount reports the live SSE connections. Used by the shutdown path
// and by tests, never by a turn.
func (h *webNoticeHub) subscriberCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// ensureNoticeHub returns this server's hub, creating it on first use. It is
// lazy so the ZERO VALUE of webServer stays usable: several existing tests
// build a `&webServer{addr: ":0"}` with no agent at all, and the events route
// must answer them rather than panic on a nil hub.
func (s *webServer) ensureNoticeHub() *webNoticeHub {
	s.noticeHubMu.Lock()
	defer s.noticeHubMu.Unlock()
	if s.noticeHub == nil {
		s.noticeHub = newWebNoticeHub()
	}
	return s.noticeHub
}

// webNoticeLoop is the notice goroutine's handle: `stop` ends it, `done` closes
// when it has returned (so the shutdown path JOINS it rather than abandoning a
// ticker — the suite's close-out sweep counts leaked goroutines).
type webNoticeLoop struct {
	stop chan struct{}
	done chan struct{}
}

// startInboxNoticeLoop starts the ONE goroutine this carrier adds: the idle
// wait point plus the notice turn. It is idempotent, so a second call (a test
// that starts a server twice, or a future restart path) cannot install a second
// poller on the same process-wide wake channel.
//
// WHY A GOROUTINE AND NOT A TICK ON SOMETHING ALREADY RUNNING: `gopi web` has no
// resident worker or merger to ride, and Rule 35 forbids inventing one (a cron
// line, a daemon, a sidecar). This goroutine is owned by the server that serves
// the requests: it is created after the agent exists and joined before runWeb
// returns, so its lifetime is the process's own and no file, socket or schedule
// outlives it.
func (s *webServer) startInboxNoticeLoop() {
	if s.agent == nil {
		return
	}
	s.noticeStartOnce.Do(func() {
		loop := &webNoticeLoop{stop: make(chan struct{}), done: make(chan struct{})}
		s.noticeLoop = loop
		go s.inboxNoticeLoop(loop)
	})
}

// stopInboxNoticeLoop ends the goroutine and WAITS for it. Safe to call when the
// loop was never started, and safe to call twice.
//
// ORDER MATTERS AT SHUTDOWN and is the caller's: `shuttingDown` is set and
// abandonInFlightTurn has already cancelled a live run, because a notice turn
// holds s.mu for its whole model round-trip and this join would otherwise wait
// for a peer that may never answer.
func (s *webServer) stopInboxNoticeLoop() {
	loop := s.noticeLoop
	if loop == nil {
		return
	}
	s.noticeStopOnce.Do(func() { close(loop.stop) })
	<-loop.done
}

// inboxNoticeLoop is the idle wait point itself. It owns two things and nothing
// else:
//
//  1. the POLLER — `SessionInboxIdleLoad` immediately (so a message delivered
//     while the server was not running at all mints a turn at once rather than
//     after one interval), then `SessionInboxIdlePoll(stop)` for the cadence.
//     Both are the agent package's, so this carrier does not own an interval,
//     a merge, or a second resolution of the session's inbox path;
//  2. the WAIT — one `select` on the agent's wake channel, which is the browser
//     carrier's substitute for the REPL's parked select over stdin. A wake is
//     only a NUDGE: runInboxNoticeTurn re-reads the LEVEL
//     (tools.JobNoticePending) before it mints anything.
func (s *webServer) inboxNoticeLoop(loop *webNoticeLoop) {
	defer close(loop.done)
	ag := s.agent
	pollDone := make(chan struct{})
	go func() {
		defer close(pollDone)
		ag.SessionInboxIdleLoad()
		ag.SessionInboxIdlePoll(loop.stop)
	}()
	defer func() { <-pollDone }()

	for {
		select {
		case <-loop.stop:
			return
		case <-agent.SessionInboxIdleWake():
			s.runInboxNoticeTurn()
		}
	}
}

// runInboxNoticeTurn answers one wake: if the LEVEL owes a turn, run it — on
// this server's ONE persistent agent, under the SAME s.mu every /api/chat turn
// takes, streaming to the hub instead of to a request writer.
//
// RETURNING WITHOUT A TURN IS THE COMMON CASE and each of the four reasons is
// deliberate:
//
//   - the server is shutting down (its session is being closed);
//   - the LEVEL is false — a stale token (a round boundary drained the queue
//     first, or a peek-only read left nothing pending), and minting a turn from
//     it would open a turn whose only content is a notice that no longer exists;
//   - a turn is IN FLIGHT (TryLock): the notice stays in the queue where it has
//     always waited, and that turn's own boundary injects it. Nothing is
//     dropped and nothing is rewritten;
//   - the queue emptied between the LEVEL and the peek (another boundary took
//     it) — same conclusion as the second case.
func (s *webServer) runInboxNoticeTurn() {
	ag := s.agent
	if ag == nil || s.shuttingDown {
		return
	}
	if !tools.JobNoticePending() {
		return
	}
	if !s.mu.TryLock() {
		return // a turn in flight: never interrupt it, never queue behind it here
	}
	defer s.mu.Unlock()
	// Re-check UNDER the lock: the turn that just released it may have drained
	// the queue at its own boundary, and a turn opened for nothing is exactly
	// the "answer with no question" this arm must not produce.
	if s.shuttingDown || !tools.JobNoticePending() {
		return
	}
	notices := tools.PeekJobNotices()
	if len(notices) == 0 {
		return
	}

	// THE TURN BEGINS (cli_web_state.go) — the same two calls handleChat makes,
	// so a server-driven turn is visible in the badge exactly like a POSTed one.
	s.state.enterTurn(webNowUnix())
	s.publishWebState()
	defer func() {
		s.state.leaveTurn(webNowUnix())
		// THE IDLE CLOCK STARTS HERE (cli_web_queue.go's webQueueAbandonAfter):
		// a queued message is abandoned only after a turn-end went by with
		// nothing claiming it, so this is the one place that reading is stamped.
		s.queue.noteTurnEnd(webNowUnix())
		s.publishWebState()
	}()

	// THE OPENING LINE IS THE AGENT'S, never this carrier's — the same function
	// `gopi chat` uses, so a web notice turn and a console notice turn are the
	// same statement of the same fact.
	msg := agent.ChatJobNoticeOpenLine(notices)
	hub := s.ensureNoticeHub()
	hub.publish(webEventNotice, webNoticePayload(msg, jobNoticeTurnLabel(notices)))
	// THE TURN BANNER FOR A SERVER-STARTED TURN (cli_web_turn.go): the number is
	// the session's own ordinal for the turn this delivery opens, published
	// before the model runs and BEFORE the notice's own frames, because a
	// delivery IS one complete input and therefore its own turn (the operator's
	// 2026-10-27 ruling) — a reader must not have to know who opened the turn to
	// read its number, exactly as with the badge.
	//
	// ITS MOMENT IS MINTED HERE AND SHARED, exactly as the POSTed path does it:
	// the frame carries it to every connected page AND the agent is given the
	// same instant for the row it is about to write, so a server-started turn's
	// banner states the moment its own record states.
	noticeTurnOpenedAt := time.Now()
	noticeTurnNo, noticeIsTurn := s.webTurnOrdinal(msg, "")
	if noticeIsTurn {
		hub.publish(webEventTurn, webTurnPayload(noticeTurnNo, noticeTurnOpenedAt))
	}
	ag.TurnOpenedAt = noticeTurnOpenedAt

	// Stream this turn's frames to the hub. The callbacks are installed and
	// restored inside the turn's lock domain, exactly as handleChat does, so no
	// other turn can observe them.
	prevOnText := ag.OnText
	prevOnToolEvents := ag.OnToolEvents
	ag.OnText = func(delta string) {
		if delta == "" {
			return
		}
		payload, _ := json.Marshal(map[string]string{"text": delta}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		hub.publish(webEventDelta, payload)
	}
	ag.OnToolEvents = func(ev agent.ToolEvent) {
		payload, err := json.Marshal(ev)
		if err != nil {
			return
		}
		hub.publish(webEventTool, payload)
		// Same status transition as a POSTed turn: a server-driven turn is a turn,
		// and a reader must not have to know who opened it to read the badge.
		s.noteToolState(ev)
	}
	defer func() {
		ag.OnText = prevOnText
		ag.OnToolEvents = prevOnToolEvents
	}()

	// The SAME per-turn budget every POSTed turn runs under (B12).
	prevTurnTimeout := ag.Timeout
	ag.Timeout = webTurnTimeout()
	defer func() { ag.Timeout = prevTurnTimeout }()

	carrier := s.webChatCarrier()
	seed := s.history
	if seed == nil {
		seed = []deepseek.Message{}
	}

	var result string
	var runErr error
	if s.tools {
		result, runErr = ag.ChatOpsOnCarrier(carrier, msg, seed)
	} else {
		result, runErr = ag.ChatOnCarrier(carrier, msg, seed)
	}

	doneBody := map[string]interface{}{"result": result}
	// THE TERMINAL FRAME REPEATS THE SAME PAIR the opening frame carried — the
	// same two variables, not a re-derivation: a reconnect that dropped the
	// turn frame gets the record's ordinal and moment here.
	if noticeIsTurn {
		doneBody[webSessionTurnField] = noticeTurnNo
		doneBody[webSessionTurnOpenedField] = sessionevent.FormatTurnStamp(noticeTurnOpenedAt)
	}
	if runErr != nil {
		doneBody["error"] = runErr.Error()
	}
	done, _ := json.Marshal(doneBody) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	hub.publish(webEventDone, done)

	// The same bookkeeping handleChat does after every turn: the server-owned
	// transcript, the run counters, and the CHATS row. The row's first-message
	// SOURCE is `notify` — the truth about who opened this conversation — so a
	// reader can tell "the operator typed the first line" from "a delivery did"
	// (internal/inputsource owns that vocabulary).
	s.iterCount++
	if ag.Cost != nil {
		s.totalTokens = ag.Cost.TotalPromptTokens + ag.Cost.TotalCompletionTokens
	}
	s.history = append([]deepseek.Message{}, ag.Ctx.Messages...)
	if runErr == nil && ag.Cost != nil {
		ag.RegisterChatSession(tools.ChatModeWeb, msg, inputsource.Notify)
		ag.RecordChatTurn(ag.Cost.TotalPromptTokens, ag.Cost.TotalCompletionTokens, ag.Cost.TotalCacheHitTokens+ag.Cost.TotalCacheMissTokens)
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "[gopi web] notice turn failed: %v\n", runErr)
	}
}

// webNoticePayload is the `notice` frame's body: the opening line the turn was
// started for, plus the label the operator reads so "why did the page answer on
// its own?" has an answer that is not "a background job finished" when no job
// existed.
func webNoticePayload(text, label string) []byte {
	p, _ := json.Marshal(map[string]string{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"text":  text,
		"label": label,
	})
	return p
}

// handleWebEvents is the standing SSE endpoint the page connects to with
// EventSource: it holds the connection open and streams every frame of every
// turn this SERVER started by itself. It sends nothing until a notice arrives
// (plus one comment keepalive per interval, which keeps an intermediary from
// reaping an idle connection).
//
// WHY THE PAGE NEEDS IT AT ALL: a server-driven turn has no request to answer.
// Its frames have nowhere to go unless something is already listening, which is
// exactly the shape of the wait point the REPL gets from its parked select.
func (s *webServer) handleWebEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed (GET "+webEventsPath+")", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "{\"error\":\"streaming unsupported\"}", http.StatusInternalServerError)
		return
	}
	writeSSEHeaders(w)
	flusher.Flush()

	hub := s.ensureNoticeHub()
	frames, release := hub.subscribe()
	defer release()

	ticker := time.NewTicker(webEventsKeepAliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := io.WriteString(w, webEventsKeepAliveComment); err != nil {
				return // the browser is gone; the request context will say so too
			}
			flusher.Flush()
		case frame, ok := <-frames:
			if !ok {
				return
			}
			writeSSEFrame(w, frame.event, frame.data)
			flusher.Flush()
		}
	}
}
