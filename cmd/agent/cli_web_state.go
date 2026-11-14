package main

// cli_web_state.go — the WEB surface's SERVER-SIDE STATUS: which of the four
// states the assistant is in right now, who can read it, and how it reaches
// the page.
//
// THE GAP THIS CLOSES. `gopi chat` draws a status line for itself (working /
// ready) because it owns the console it writes to. `gopi web` writes to
// whatever browser happens to be attached, and it had no state at all: a reader
// looking at the page could not tell an idle assistant from one that is
// answering from one that is parked inside a tool call, and could not tell
// whether the turn in flight could be abandoned. The page's own `busy` flag is
// a CLIENT guess: it is set when a POST is sent and cleared in a `finally`
// block, so it says nothing about a turn the SERVER started by itself
// (cli_web_notice.go's notice turn), and it cannot name the tool.
//
// WHERE THE STATE COMES FROM — and what is NOT invented. Every transition below
// is driven by a callback the agent loop ALREADY fires, or by a point in the
// carriers the page already talks to:
//
//   - a turn STARTS   → the point where handleChat / runInboxNoticeTurn takes
//                       the turn lock and installs its callbacks;
//   - a first DELTA   → the `OnText` callback that already streams `delta`;
//   - a TOOL call     → `OnToolEvents`' ToolEventKindCall event, BEFORE the
//                       tool runs (so the state is on screen while it blocks,
//                       which is the whole point — a state that appeared at
//                       the RESULT would be a state nobody ever sees);
//   - back to THINKING → the matching ToolEventKindResult, when no other call is
//                       outstanding;
//   - a turn ENDS     → the carrier's own end (defer / after the call returns),
//                       whatever the outcome.
//
// NOTHING NEW SCHEDULES. There is no ticker, no poller, no goroutine and no
// file here (Rule 35): the state machine is a mutex plus four counters, and the
// state CHANGES ride the ONE SSE connection the page already holds for
// server-started turns (`/api/events`, cli_web_notice.go's hub). A page that
// opened no EventSource still gets the truth on demand from `/api/state`.
//
// INTERRUPTIBILITY. A state is interruptible when this server holds a live,
// abandonable run — i.e. the agent's own `InterruptRun()` has something to
// cancel. Rather than duplicate that predicate, the machine asks the agent
// exactly what the interrupt route would ask (`agentRunInterruptible`), so the
// badge the reader sees and the button the page offers cannot disagree.

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
)

// The four states, as the wire spells them (Rule 1b/13/14: the page carries its
// own copies inside its Go raw string, and the regression test pins every pair
// rather than trusting a comment).
const (
	// webStateIdle — no turn is running: the assistant is waiting for a message.
	webStateIdle = "idle"
	// webStateResponding — a turn is running and the model is producing text
	// (it has emitted at least one delta, and no tool call is outstanding).
	webStateResponding = "responding"
	// webStateToolRunning — the turn is parked inside a tool call. The state
	// carries the tool's name, because "waiting" without a noun is what the
	// page already had.
	webStateToolRunning = "tool_running"
)

// webStateFrame is the SSE frame that CARRIES a transition. It differs from
// every other frame on the stream in one way that matters: it carries the
// WHOLE state, never a delta. A subscriber that missed an earlier transition
// (the hub drops a frame for a subscriber that stopped reading — see
// webNoticeDropReport) therefore cannot render a stale state: the next frame
// is self-sufficient, and the snapshot at connect time is the same shape.
const webStateFrame = "state"

// webStatePath is the snapshot endpoint. It exists because a status badge that
// can only be OBSERVED by watching the stream cannot be CHECKED: this is the
// reading a regression test and an acceptance run take, and it is the same
// document the `state` frame carries (one shape, one encoder).
const webStatePath = "/api/state"

// webStateNoCache is the snapshot's Cache-Control. The whole value of the
// endpoint is that a reader learns the state RIGHT NOW, so a cached copy is the
// one answer that is always wrong.
const webStateNoCache = "no-store"

// webStateReport prints a state-machine fault. It exists so a failure on this
// path is never silent (Rule 15): a badge that quietly stopped updating is the
// worst of both worlds — the page looks authoritative and is stale.
const webStateReport = "[gopi web] state: "

// webStateSnapshot is the whole status, as the page and the snapshot endpoint
// both read it.
//
// `omitempty` is used for NOTHING here (Rule 15a): every field is required, and
// an empty `tools` list must serialise as `[]`, not vanish — a consumer that
// decoded "no field" as "no information" would be reading the wrong thing.
type webStateSnapshot struct {
	// State is one of webStateIdle / webStateResponding / webStateToolRunning.
	State string `json:"state"`
	// Tool is the tool currently running (empty unless State is
	// webStateToolRunning) — "waiting" with no noun is what the page had before.
	Tool string `json:"tool"`
	// Tools lists EVERY call outstanding in this turn. It is a list rather than
	// a scalar because the loop may emit several calls before their results
	// arrive; Tool is its head, so a reader never has to derive one from the
	// other.
	Tools []string `json:"tools"`
	// Interruptible reports whether a live run can be abandoned right now — the
	// same predicate the /api/chat/interrupt route acts on.
	Interruptible bool `json:"interruptible"`
	// Turn is how many turns this server has started (POSTed and server-driven
	// alike). A reader compares it across two reads to tell "nothing happened"
	// from "the same state, a new turn".
	Turn int `json:"turn"`
	// Since is the unix second the CURRENT state was entered. A long turn is
	// therefore visibly long instead of merely "busy".
	Since int64 `json:"since"`
	// QueueState is this surface's MID-TURN state, derived from the messages
	// waiting on the turn lock (cli_web_queue.go). It is a SEPARATE field
	// rather than a value State can take: a queued message does not stop the
	// turn in flight from being `tool_running`, and overwriting State would
	// destroy the reading this machine was built for. "" when nothing waits;
	// otherwise webStateQueued (something is waiting its turn) or
	// webStateSteering (the wait addresses the running turn instead).
	QueueState string `json:"queue_state"`
	// Pending is the queue itself, in arrival order: each entry's id, typed
	// state/placement, position among the queued entries, preview and whether
	// the withdrawal route will accept it. Always a list (Rule 15a) — an empty
	// queue serialises as `[]`, never as a missing field.
	Pending []webQueueEntry `json:"pending"`
}

// webStateMachine is the state itself: a mutex, four counters and the clock
// reading of the last transition. It has no lifecycle — the zero value is a
// valid IDLE machine, so every existing `&webServer{...}` literal keeps working
// without an edit (the same reason webNoticeHub is created lazily).
type webStateMachine struct {
	mu sync.Mutex
	// state is the current state; "" is the zero value and means IDLE, so a
	// server that never ran a turn reports webStateIdle without a constructor.
	state string
	// tools is the outstanding-call stack: a call is appended when it is
	// announced and removed when its result arrives. Several may be
	// outstanding at once, which is why the wire carries a list.
	tools []string
	// turns counts started turns; since is the unix second of the last
	// transition.
	turns int
	since int64
}

// snapshot renders the current state. `interruptible` is an ARGUMENT rather
// than a field: it is a fact about the AGENT (a live run it can cancel), not
// about this machine, and asking the agent under this mutex would be a lock
// order nobody wrote down.
func (m *webStateMachine) snapshot(interruptible bool, now int64) webStateSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	st := m.state
	if st == "" {
		st = webStateIdle
	}
	// The tool name is the HEAD of the outstanding list, and the list is never
	// empty while the state is tool_running — enterTool appends before it sets
	// the state, so the two cannot disagree.
	tool := ""
	if len(m.tools) > 0 {
		tool = m.tools[0]
	}
	tools := make([]string, len(m.tools))
	copy(tools, m.tools)
	since := m.since
	if since == 0 {
		since = now
	}
	return webStateSnapshot{
		State:         st,
		Tool:          tool,
		Tools:         tools,
		Interruptible: interruptible,
		Turn:          m.turns,
		Since:         since,
	}
}

// turnInFlight reports whether a turn this server started is still registered.
// It is the machine's own statement, taken under the same mutex as every
// transition, so the answer cannot be a reading of a state that just changed.
func (m *webStateMachine) turnInFlight() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state != "" && m.state != webStateIdle
}

// enterTurn starts a turn: the state becomes RESPONDING and the outstanding
// call stack is cleared. A turn that begins while another is somehow still
// registered therefore cannot inherit the previous turn's tool — the stack is
// reset, not appended to.
func (m *webStateMachine) enterTurn(now int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turns++
	m.tools = m.tools[:0]
	m.state = webStateResponding
	m.since = now
}

// enterTool records one outstanding call and puts the machine into
// TOOL_RUNNING. Called on the CALL event, i.e. BEFORE the tool executes, so the
// state is true for the whole of the tool's blocking duration.
func (m *webStateMachine) enterTool(name string, now int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = append(m.tools, name)
	m.state = webStateToolRunning
	m.since = now
}

// leaveTool records one result. The machine returns to RESPONDING only when NO
// call is outstanding any more: a result for call 1 of 2 must not claim the
// turn is answering while call 2 still blocks.
func (m *webStateMachine) leaveTool(now int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.tools) > 0 {
		m.tools = m.tools[1:]
	}
	if len(m.tools) > 0 {
		m.state = webStateToolRunning
	} else {
		m.state = webStateResponding
	}
	m.since = now
}

// leaveTurn ends the turn: IDLE, and the call stack is dropped (a turn that
// ended mid-call leaves nothing outstanding to name).
func (m *webStateMachine) leaveTurn(now int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tools = m.tools[:0]
	m.state = webStateIdle
	m.since = now
}

// webNowUnix is the clock reading every transition stamps. Seconds: the page
// renders "how long has it been like this", and a finer unit would be a number
// nothing acts on.
func webNowUnix() int64 { return time.Now().Unix() }

// agentRunInterruptible reports whether the turn THIS SERVER is showing can be
// abandoned right now: a turn is registered as running (the machine says so) AND
// the agent holds a live run.
//
// BOTH HALVES ARE KEPT, and each answers a different question. `RunInFlight()`
// is the agent's own probe — "a run exists right now that InterruptRun could
// abandon" — and since task 01a1b4a2 it is RETIRED when the run returns
// (`endRunScope`), so it no longer answers true forever after the first turn.
// The state machine's `state` is this SURFACE's claim that the turn it is
// showing is the one in flight; a server that has started no turn can still see
// a live run it did not start (a notice round, a slash command), and the badge
// must describe the turn the reader is looking at. The pair is the honest
// reading: no turn of ours ⇒ nothing for the stop button to abandon.
func agentRunInterruptible(inTurn bool, ag *agent.Agent) bool {
	return inTurn && ag.RunInFlight()
}

// webStateSnapshotFor renders this server's status right now. It is the ONE
// producer of the document: the snapshot endpoint and the SSE frame both call
// it, so the two cannot describe the server differently.
func (s *webServer) webStateSnapshotFor() webStateSnapshot {
	// One read of the machine answers both questions: `running` is the surface's
	// own claim that a turn is in flight, and the agent's live-run probe is only
	// consulted while that claim holds.
	running := s.state.turnInFlight()
	snap := s.state.snapshot(agentRunInterruptible(running, s.agent), webNowUnix())
	// The waiting messages are the SECOND half of the same document, read under
	// the queue's own mutex (it is mutated by requests blocked on s.mu, which
	// this function's caller may be holding). One producer, two mutexes, no
	// second document.
	//
	// The queue is rendered AGAINST THIS SNAPSHOT'S OWN state reading (State /
	// Since came out of the same lock as the state above), so a message whose
	// turn ended without it ever being claimed stops being reported as waiting
	// at the very instant the turn that would have run it is over — the badge
	// and the state it sits beside cannot tell different stories.
	snap.Pending = s.queue.entriesFor(running, webNowUnix())
	snap.QueueState = webQueueStateOf(snap.Pending)
	return snap
}

// webQueueStateOf derives the mid-turn state from the waiting entries: nothing
// waits ⇒ "" (the field is empty, not a state); at least one QUEUED entry ⇒
// webStateQueued; only steering entries ⇒ webStateSteering. A steering entry is
// reported only when nothing is queued, because "waiting its turn" is the
// stronger statement — the queue's own order is what the reader acts on.
func webQueueStateOf(pending []webQueueEntry) string {
	if len(pending) == 0 {
		return ""
	}
	for _, e := range pending {
		if e.State == webStateQueued {
			return webStateQueued
		}
	}
	return webStateSteering
}

// noteToolState advances the machine from ONE tool event and publishes the
// result. It is what both carriers call from `OnToolEvents` — one transition
// rule, two carriers (Rules 14/15), so a POSTed turn and a server-driven turn
// cannot report different statuses for the same loop event.
//
// A RESULT for a call nobody announced (a turn that began mid-call, or an event
// arriving after its turn ended) is IGNORED rather than allowed to pop an empty
// stack: leaveTool already refuses to go below zero, and the state it leaves
// behind is the truthful one.
func (s *webServer) noteToolState(ev agent.ToolEvent) {
	switch ev.Kind {
	case agent.ToolEventKindCall:
		s.state.enterTool(ev.Name, webNowUnix())
	case agent.ToolEventKindResult:
		s.state.leaveTool(webNowUnix())
	default:
		// An unknown kind is not a status transition. Reporting it would invent a
		// state for an event this machine has no rule for (Rule 15: silence about
		// something that never happened is not a silent failure).
		return
	}
	s.publishWebState()
}

// publishWebState renders the state and pushes it as ONE `state` frame to every
// live subscriber of the standing stream (cli_web_notice.go's hub). Called at
// every transition, from inside the turn that caused it — so the page learns of
// a transition at the moment it happens rather than at the turn's end.
//
// A frame that cannot be encoded is not published and NOT swallowed silently:
// the encode is of a value built in-process (strings and ints), so it cannot
// fail; the branch exists so a future field with a func or channel in it fails
// loudly at review rather than silently emptying the badge.
func (s *webServer) publishWebState() {
	snap := s.webStateSnapshotFor()
	payload, err := json.Marshal(snap)
	if err != nil {
		fmt.Fprintf(os.Stderr, webStateReport+"cannot encode the state frame: %v\n", err)
		return
	}
	s.ensureNoticeHub().publish(webStateFrame, payload)
}

// handleWebState is the snapshot endpoint: GET → the same JSON the `state`
// frame carries. It takes NO lock (the machine has its own) and does not touch
// the turn lock, so a reader can ask while a turn is parked inside a tool —
// which is exactly when the question is worth asking.
func (s *webServer) handleWebState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed (GET "+webStatePath+")", http.StatusMethodNotAllowed)
		return
	}
	payload, err := json.Marshal(s.webStateSnapshotFor())
	if err != nil {
		fmt.Fprintf(os.Stderr, webStateReport+"cannot encode the state snapshot: %v\n", err)
		http.Error(w, "{\"error\":\"state unavailable\"}", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", webStateNoCache)
	w.WriteHeader(http.StatusOK)
	w.Write(payload) //stylegate:ignore a failed write to a status snapshot is the client's disconnect, not a server fault
}
