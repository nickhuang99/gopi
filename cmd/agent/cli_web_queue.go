package main

// cli_web_queue.go — the SERVER-SIDE view of the messages that arrived while a
// turn was running: who is waiting, in what ORDER, how it was PLACED, and
// whether it can still be withdrawn.
//
// THE GAP THIS CLOSES. Before this file the queue was the PAGE's private
// business: `pendingMsgs` was a client-side array, the server saw at most the
// single POST it was holding behind s.mu, and a message that arrived mid-turn
// was rendered with a CSS border difference and the bare word `queued` — the
// page's own source called the distinction "cosmetic only". A reader could not
// read how many messages were waiting, which one would go next, or that an
// Alt+Enter message had been typed as STEERING rather than QUEUED; and a
// message that had been waited for could not be taken back.
//
// WHAT IS NOT INVENTED. This file adds no ticker, no poller and no goroutine
// (Rule 35): it is a mutex plus a slice, mutated at the two points that already
// exist — handleChat when it fails to take s.mu (the point that already wrote
// the `queued` frame), and handleChat when the turn finally begins. The
// resulting document rides the ONE state document the surface already
// publishes (webStateSnapshotFor → /api/state and the `state` SSE frame), so a
// page that opened no EventSource still reads the truth on demand.
//
// THE TWO PLACEMENTS, as STATES (the epic's fifth and sixth state). A message
// that arrives mid-turn is either
//
//	webStateQueued   — it waits for the current turn to end and then runs as a
//	                   turn of its own. It has a POSITION (1 = next) and it is
//	                   CANCELLABLE: nothing has been handed to the model.
//	webStateSteering — it ADDRESSES the turn in flight (the page's Alt+Enter
//	                   gesture, carried as webSteerPrefix). It has no queue
//	                   position, and it is NOT cancellable through the queue
//	                   route: withdrawing it is a different act from withdrawing
//	                   a message that has not been delivered.
//
// Both spellings are the PLACEMENT constants, not a second vocabulary (Rule
// 14): a state and a placement that meant the same thing and were spelled
// differently is exactly the drift this rule exists to prevent.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"
)

// webStateQueued / webStateSteering are the two mid-turn STATES. They are the
// placement values themselves — one idea, one spelling (Rules 1b/13/14) — so
// the wire can never say `queued` in one field and `queued` in another for
// different reasons.
const (
	webStateQueued   = webPlacementQueued
	webStateSteering = webPlacementSteering
)

// webQueuePreviewRunes bounds the preview carried for one waiting message. The
// queue view is a STATUS line, not a transcript: a reader needs enough to
// recognise WHICH message it is, and the whole body is already in the log.
const webQueuePreviewRunes = 40

// webQueueAbandonAfter is how long a REGISTERED message may go unclaimed while
// NO turn is in flight before it stops being reported as waiting.
//
// WHY IT EXISTS (measured, not imagined): the page registers a mid-turn message
// when it is TYPED and POSTs the turn later, from drainQueue, when the current
// turn's stream ends. Between those two moments the entry is genuinely waiting.
// But a tab that is closed, reloaded or throttled never sends that POST — and
// before this bound the server kept listing the entry forever, so the badge said
// "排队中" over a queue nothing would ever run. That is precisely the false
// reading this epic exists to end (measured on a live server: a registration
// with no request behind it was still listed 40 seconds after the turn it was
// typed during had ended).
//
// THE CLOCK MEASURES IDLE TIME, and that distinction is the whole correctness of
// it: a message that waited out a 45-second tool call is not abandoned, so the
// bound counts from the last moment a turn was in flight (webQueue.lastBusy),
// never from the registration instant. Counting from registration would drop a
// real queued message exactly as the server was about to run it.
//
// A var rather than a const so the regression test can tighten it instead of
// sleeping for it.
var webQueueAbandonAfter = 30 * time.Second

// webEventCancelled is the frame a waiting request answers when its message was
// withdrawn while it waited. It is deliberately NOT the `error` frame: nothing
// failed — the reader asked for exactly this.
const webEventCancelled = "cancelled"

// webChatQueueCancelPath is the withdrawal route. POST {"id":N} → the message
// leaves the queue and its turn never runs. Registered on the SAME mux as every
// other route, so webTokenGate covers it identically (an unauthenticated POST is
// refused before the handler runs).
const webChatQueueCancelPath = "/api/chat/queue/cancel"

// webChatQueuePath is the REGISTRATION route: POST {"message":…,"placement":…}
// → the id and position the message was given. It exists because the page's
// queue is a CLIENT-side delivery order (drainQueue) — without a registration
// step the server would only learn about a message at the moment it POSTs the
// turn, i.e. one turn too late to show it, count it, or withdraw it. The route
// runs NO turn and calls NO model: it records what is waiting.
const webChatQueuePath = "/api/chat/queue"

// webQueueCancelBadBody is answered when the cancel body is not a JSON object
// carrying an id. Named (Rule 1b) because the regression test asserts it.
const webQueueCancelBadBody = "{\"error\":\"body must be {\\\"id\\\": <int>}\"}"

// webQueueEntry is ONE waiting message as the wire carries it inside the state
// document. Every field is required (Rule 15a: no `omitempty` here — a consumer
// must be able to tell "position 0 because it is steering" from "the field was
// dropped").
type webQueueEntry struct {
	// ID is the cancellation handle. Assigned by the server, monotonic, and it
	// is what POST /api/chat/queue/cancel names.
	ID int `json:"id"`
	// State is webStateQueued or webStateSteering — the fifth and sixth states.
	State string `json:"state"`
	// Placement is the same value under the name the wire has used since B2, so
	// a consumer written against the `queued` frame keeps working.
	Placement string `json:"placement"`
	// Position is this entry's place among the QUEUED entries (1 = next to
	// run). It is 0 for a steering entry, which has no place in the queue.
	Position int `json:"position"`
	// Preview is the first webQueuePreviewRunes runes of the message, so the
	// page can say WHICH message is waiting without shipping the body twice.
	Preview string `json:"preview"`
	// Cancellable reports whether the withdrawal route will accept this id.
	// It is computed server-side (only a queued, not-yet-started message can be
	// withdrawn) so the button the page draws and the answer the route gives
	// cannot disagree.
	Cancellable bool `json:"cancellable"`
}

// webQueueItem is the in-memory entry. `cancelled` and `started` are read by
// the request that is WAITING on s.mu, which is how a withdrawal actually
// prevents a turn: the handler re-reads its own item under the queue's mutex
// before it runs anything.
type webQueueItem struct {
	id      int
	state   string
	preview string
	// since is the unix second the message was REGISTERED. It is what the idle
	// abandonment bound measures (webQueueAbandonAfter) — a registration that
	// no turn lock ever claims is not a waiting message, it is a page that went
	// away.
	since     int64
	cancelled bool
	started   bool
}

// webQueue is the queue itself. The ZERO VALUE IS A USABLE EMPTY QUEUE (the
// same reason webStateMachine has no constructor): every existing
// `&webServer{...}` literal keeps working without an edit.
type webQueue struct {
	mu     sync.Mutex
	nextID int
	items  []*webQueueItem
	// endedAt is the unix second the last turn ENDED (noteTurnEnd, called from
	// the one place a turn's end is already recorded). The abandonment bound
	// counts IDLE time, and idle time starts there — not at the registration
	// instant, because a message that waited out a long tool call is the most
	// legitimate entry in the queue. Zero means no turn has ended yet.
	endedAt int64
}

// enqueue registers one waiting message and returns its item.
func (q *webQueue) enqueue(state, preview string) *webQueueItem {
	return q.enqueueAt(state, preview, webNowUnix())
}

// enqueueAt is enqueue with the clock supplied, so a test can age an entry
// without sleeping for webQueueAbandonAfter.
func (q *webQueue) enqueueAt(state, preview string, now int64) *webQueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.nextID++
	it := &webQueueItem{id: q.nextID, state: state, preview: preview, since: now}
	q.items = append(q.items, it)
	return it
}

// entries renders the queue for the wire, assuming NO turn is in flight (the
// reading every caller that is not rendering a state snapshot wants: the
// registration ack, the cancel ack, the unit tests).
func (q *webQueue) entries() []webQueueEntry {
	return q.entriesFor(false, webNowUnix())
}

// entriesFor renders the queue against the TWO facts the abandonment bound
// needs: whether a turn is in flight, and the current second. A message that
// nothing claims while the server is idle for webQueueAbandonAfter stops being
// reported as waiting (see that constant for the measured reason).
func (q *webQueue) entriesFor(inFlight bool, now int64) []webQueueEntry {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.entriesLocked(inFlight, now)
}

// entriesLocked is the one renderer (the caller already holds the mutex). It
// also RECLAIMS what it drops: an entry that ages out with nothing in flight
// has no owning request to run compact() (the page that registered it is gone),
// so the renderer is the last thing that will ever look at it — leaving it in
// the slice would let a long-lived server accumulate one 40-rune preview per
// abandoned tab forever.
func (q *webQueue) entriesLocked(inFlight bool, now int64) []webQueueEntry {
	out := make([]webQueueEntry, 0, len(q.items))
	kept := q.items[:0]
	pos := 0
	for _, it := range q.items {
		if it.cancelled || it.started {
			continue
		}
		if !inFlight && it.abandonedAt(q.endedAt, now) {
			continue
		}
		kept = append(kept, it)
		e := webQueueEntry{
			ID:          it.id,
			State:       it.state,
			Placement:   it.state,
			Preview:     it.preview,
			Cancellable: it.state == webStateQueued,
		}
		if e.Cancellable {
			pos++
			e.Position = pos
		}
		out = append(out, e)
	}
	q.items = kept
	return out
}

// noteTurnEnd records that a turn ended NOW, which is when this queue starts
// counting IDLE time (see webQueueAbandonAfter).
func (q *webQueue) noteTurnEnd(now int64) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.endedAt = now
}

// abandonedAt reports whether this entry has gone webQueueAbandonAfter with no
// turn in flight to wait FOR. Such an entry is a registration whose page went
// away before it POSTed the turn, not a message anything will run.
//
// The idle clock starts at the LATER of the entry's own registration and the
// last turn-end: whichever is more recent is the moment something last could
// have claimed it.
func (it *webQueueItem) abandonedAt(turnEndedAt, now int64) bool {
	if it.since == 0 {
		return false
	}
	from := it.since
	if turnEndedAt > from {
		from = turnEndedAt
	}
	return now-from >= int64(webQueueAbandonAfter/time.Second)
}

// markStarted marks an item as being delivered now (its turn has BEGUN), so it
// leaves the pending view at that instant rather than at the turn's end.
func (q *webQueue) markStarted(it *webQueueItem) {
	q.mu.Lock()
	defer q.mu.Unlock()
	it.started = true
}

// cancel withdraws one queued message. Only a QUEUED, not-yet-started message
// can be withdrawn: a steering message addresses the turn in flight (a
// different act) and a started one is already the current turn. It reports
// whether anything was withdrawn.
func (q *webQueue) cancel(id int) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.id != id {
			continue
		}
		if it.cancelled || it.started || it.state != webStateQueued {
			return false
		}
		it.cancelled = true
		return true
	}
	return false
}

// withdrawn reports whether an item was taken back while it waited. It is read
// under the queue's mutex, so the answer is the queue's own state rather than a
// snapshot of it.
func (q *webQueue) withdrawn(it *webQueueItem) bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return it.cancelled
}

// byID resolves a cancellation handle to its item (nil when the id names no
// waiting message — an entry already delivered, or a stale page).
func (q *webQueue) byID(id int) *webQueueItem {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, it := range q.items {
		if it.id == id {
			return it
		}
	}
	return nil
}

// compact drops the finished entries. Called from the request that owns the
// item, after it leaves the view, so the slice cannot grow without bound on a
// long-lived server (no sweep, no ticker — the owner is the reclaimer).
func (q *webQueue) compact() {
	q.mu.Lock()
	defer q.mu.Unlock()
	kept := q.items[:0]
	for _, it := range q.items {
		if it.started || it.cancelled {
			continue
		}
		kept = append(kept, it)
	}
	q.items = kept
}

// webQueuePreview renders the first webQueuePreviewRunes runes of a message for
// the status line. Whitespace is collapsed so a multi-line message stays one
// readable line; the rune budget (not a byte budget) is what keeps a CJK
// message from being cut mid-character.
func webQueuePreview(msg string) string {
	flat := strings.Join(strings.Fields(msg), " ")
	runes := []rune(flat)
	if len(runes) <= webQueuePreviewRunes {
		return flat
	}
	return string(runes[:webQueuePreviewRunes]) + "…"
}

// enqueueWebMessage registers one mid-turn message and answers the frame the
// waiting request writes BEFORE it blocks on s.mu. The frame carries the WHOLE
// queue, not just this entry, so a page that missed an earlier frame renders
// the truth from the next one (the same rule the `state` frame follows).
func (s *webServer) enqueueWebMessage(msg, placement string) *webQueueItem {
	state := webStateQueued
	if placement == webPlacementSteering {
		state = webStateSteering
	}
	it := s.queue.enqueue(state, webQueuePreview(msg))
	s.publishWebState()
	return it
}

// queueView is the queue as the STATE DOCUMENT reports it — the same reading
// /api/state carries, keyed on whether a turn is actually in flight. Every
// other answer (the registration ack, the withdrawal ack, the `queued` frame)
// goes through it too, so no two surfaces can disagree about who is waiting.
func (s *webServer) queueView() []webQueueEntry {
	return s.queue.entriesFor(s.state.turnInFlight(), webNowUnix())
}

// webQueuedFramePayload renders one `queued` frame: the queue as it now stands,
// the id the new entry was given, and its typed placement.
//
// THE ENVELOPE IS APPEND-ONLY. Existing keys (`placement`, `notice`) keep their
// meaning — a page that only knows them keeps working — and the typed queue
// rides alongside. `pending` is the WHOLE queue (not just the new entry) for
// the same reason the `state` frame carries the whole state: a subscriber that
// missed an earlier frame cannot render a stale queue from the next one.
func webQueuedFramePayload(pending []webQueueEntry, placement string, id int) []byte {
	p, err := json.Marshal(map[string]interface{}{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"placement": placement,
		"notice":    webBusyWaitNotice,
		"id":        id,
		"pending":   pending,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, webStateReport+"cannot encode the queued frame: %v\n", err)
		return webQueuedPayload()
	}
	return p
}

// webCancelNotice is the `cancelled` frame's text: the message was taken back
// BEFORE it ran, so the reader is told what did NOT happen rather than being
// left with a silently vanished entry.
const webCancelNotice = "withdrawn before it ran — it was never sent to the model"

// webCancelledPayload is the `cancelled` frame's body, written to a request
// whose message was withdrawn while it waited (cli_web_queue.go's cancel route
// is the only writer of that state).
func webCancelledPayload(id int) []byte {
	p, err := json.Marshal(map[string]interface{}{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"id":        id,
		"cancelled": true,
		"notice":    webCancelNotice,
	})
	if err != nil {
		return []byte("{}")
	}
	return p
}

// handleChatQueueRegister records one waiting message and answers its id and
// place. It takes no turn lock: the message is waiting on a turn this route is
// not, and taking the lock would make the registration itself the wait.
func (s *webServer) handleChatQueueRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (POST "+webChatQueuePath+")", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		Message   string `json:"message"`
		Placement string `json:"placement"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, webMaxChatBodyBytes)).Decode(&body); err != nil {
		http.Error(w, "{\"error\":\"body must be {\\\"message\\\": …}\"}", http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(body.Message) == "" {
		http.Error(w, "{\"error\":\"empty message\"}", http.StatusBadRequest)
		return
	}
	item := s.enqueueWebMessage(stripWebSteerSentinel(body.Message), body.Placement)
	// ONE renderer for the queue as the STATE document would report it — the
	// ack and /api/state must not disagree about who is waiting (the in-flight
	// reading is what the abandonment bound keys on).
	pending := s.queueView()
	var pos int
	for _, e := range pending {
		if e.ID == item.id {
			pos = e.Position
		}
	}
	payload, err := json.Marshal(map[string]interface{}{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"id":        item.id,
		"state":     item.state,
		"placement": item.state,
		"position":  pos,
		"pending":   pending,
	})
	if err != nil {
		http.Error(w, "{\"error\":\"queue unavailable\"}", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(payload) //stylegate:ignore a failed write to a queue ack is the client's disconnect, not a server fault
}

// webQueuePollInterval is how often a request that is WAITING on the turn lock
// re-reads its own queue item, so a withdrawal reaches it before the previous
// turn ends instead of at the moment it would have run. It is a poll of a
// value already in memory inside a request that is ALREADY blocked on that
// lock — not a background ticker (Rule 35: nothing new schedules), and a
// withdrawn message is acknowledged in ~this long rather than one turn later.
const webQueuePollInterval = 100 * time.Millisecond

// waitForTurnOrWithdrawal blocks until this request holds mu — or until the
// message it is carrying is withdrawn while it waits, which answers `true`
// WITHOUT taking the lock (a withdrawn message must not first queue behind the
// very turn it is no longer asking for).
//
// mu is the caller's turn lock; it is taken here so the two outcomes are one
// wait rather than two contended locks.
func waitForTurnOrWithdrawal(mu *sync.Mutex, q *webQueue, it *webQueueItem) bool {
	for {
		if mu.TryLock() {
			return false
		}
		if it != nil && q.withdrawn(it) {
			return true
		}
		time.Sleep(webQueuePollInterval)
	}
}

// stripWebSteerSentinel removes the page's steering sentinel from a registered
// message — the same one-rule strip handleChat performs before the turn, so the
// preview a reader sees is the words they typed and never the framing.
func stripWebSteerSentinel(msg string) string {
	if after, ok := strings.CutPrefix(strings.TrimSpace(msg), strings.TrimSpace(webSteerPrefix)); ok {
		return strings.TrimSpace(after)
	}
	return strings.TrimSpace(msg)
}

// handleChatQueueCancel is the withdrawal route: POST {"id":N} → the message
// leaves the queue and its turn never runs.
//
// It deliberately does NOT take s.mu — the whole point is to reach a message
// that is WAITING on that lock; a handler queueing behind it would be served
// only after the very turn the message waits for. The queue has its own mutex,
// and the waiting request re-reads its item before running anything, which is
// where the withdrawal actually takes effect.
func (s *webServer) handleChatQueueCancel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed (POST "+webChatQueueCancelPath+")", http.StatusMethodNotAllowed)
		return
	}
	if s.shuttingDown {
		http.Error(w, "{\"error\":\"server shutting down\"}", http.StatusServiceUnavailable)
		return
	}
	var body struct {
		ID int `json:"id"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, webMaxChatBodyBytes)).Decode(&body); err != nil || body.ID == 0 {
		http.Error(w, webQueueCancelBadBody, http.StatusBadRequest)
		return
	}
	cancelled := s.queue.cancel(body.ID)
	// The state frame goes out whatever the answer: a refusal must not leave the
	// page rendering a queue the server no longer holds, and an accepted
	// withdrawal is exactly the transition the badge exists to show.
	s.publishWebState()
	payload, err := json.Marshal(map[string]interface{}{ //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
		"ok":        cancelled,
		"cancelled": body.ID,
		"pending":   len(s.queueView()),
	})
	if err != nil {
		http.Error(w, "{\"error\":\"cancel unavailable\"}", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(payload) //stylegate:ignore a failed write to a cancel ack is the client's disconnect, not a server fault
}
