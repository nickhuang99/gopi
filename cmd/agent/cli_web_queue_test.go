package main

// cli_web_queue_test.go — the regression guard for the mid-turn queue
// (cli_web_queue.go): the two placements as STATES, the queue SNAPSHOT the
// server publishes, and the WITHDRAWAL route.
//
// THE DEFECT THIS PINS. The page separated a mid-turn message's two placements
// by a CSS border and its own source called that difference "cosmetic only":
// nothing on screen said whether a message would wait its turn or address the
// run in flight, the server could not name the message at all, and a message
// that had already been waited for could not be taken back. The fix makes both
// placements first-class states in the document /api/state already carries, and
// gives a queued message an id that a POST can withdraw.
//
// WHAT THE TESTS DRIVE — the REAL surfaces:
//
//   - the real mux (webChatHandler) for the registration, the snapshot and the
//     cancel routes, so a handler that answers but is not routed fails here;
//   - a REAL turn through the agent's ReAct loop, parked inside a tool call,
//     which is the only state in which a message can be waiting at all;
//   - a second /api/chat POST that is ACTUALLY waiting on s.mu, withdrawn
//     through the route, with the model's own request count as the evidence
//     that its turn never ran.

import (
	"context"
	"encoding/json"
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

// webQueueTestToolDelay is how long the stubbed tool blocks: long enough that a
// second message can be posted, registered and withdrawn WHILE the turn is
// parked, which is the whole subject.
var webQueueTestToolDelay = 600 * time.Millisecond

// webQueueWithdrawnText is the message the withdrawal test posts and then takes
// back. It is a constant because the assertion is about THIS text never
// reaching the model — and about the transcript not growing by it.
const webQueueWithdrawnText = "withdrawn message: this text must never reach the model"

// webQueueTestPoll is the poll step while waiting for a server-side side effect
// (the queue gaining an entry, a waiting request answering its frame).
const webQueueTestPoll = 10 * time.Millisecond

// webQueueTurnServer builds a server whose model asks for ONE tool call and
// then answers, with the tool's executor blocking. The returned counter reports
// how many MODEL REQUESTS the turn has caused and the getter every request
// BODY — the evidence a withdrawn message never reached the model.
func webQueueTurnServer(t *testing.T) (*webServer, *httptest.Server, func() int, func() []string) {
	t.Helper()
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	var (
		mu     sync.Mutex
		round  int
		bodies []string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, rerr := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if rerr != nil {
			// The bodies are the evidence this suite asserts on ("the withdrawn
			// text never reached the model"), so a body that could not be read must
			// not be recorded as an empty one — it is recorded as unreadable and the
			// assertion that searches it cannot pass by accident.
			mu.Lock()
			bodies = append(bodies, "(body unreadable: "+rerr.Error()+")")
			mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		round++
		n := round
		bodies = append(bodies, string(raw))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")
		if n == 1 {
			toolUseSSE(w, "call_queue_1", webStateTestToolName, `{"path":"/etc/hostname"}`)
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
	ag.SetToolExecutor(func(tc deepseek.ToolCall) string {
		// Not a poll: this duration IS the subject. The queue tests post a second
		// message, read the server's queue view and withdraw it WHILE turn 1 is
		// parked here, so the executor has to stay parked long enough for those
		// three round trips to land (TestWebStateToolTransitionPrecedesTheResult
		// drives the same executor with the delay set to zero).
		time.Sleep(webQueueTestToolDelay) //stylegate:allow-sleep the stubbed executor must stay parked so a concurrent registration and withdrawal land inside the turn
		return "hostname-file-body"
	})

	ws := &webServer{addr: ":0", agent: ag, tools: true, startedAt: time.Now()}
	httpSrv := httptest.NewServer(ws.webChatHandler())
	t.Cleanup(httpSrv.Close)
	modelRequests := func() int {
		mu.Lock()
		defer mu.Unlock()
		return round
	}
	modelBodies := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), bodies...)
	}
	return ws, httpSrv, modelRequests, modelBodies
}

// postJSON POSTs one JSON body to a route on the real mux and returns the
// status code and decoded body.
func postJSON(t *testing.T, base, path string, body interface{}) (int, map[string]interface{}) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal %s body: %v", path, err)
	}
	res, err := http.Post(base+path, "application/json", strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer res.Body.Close() //stylegate:ignore the close of a test response has no assertion riding on it
	var out map[string]interface{}
	dec := json.NewDecoder(res.Body)
	_ = dec.Decode(&out) //stylegate:ignore a non-JSON body is reported by the caller's status assertion
	return res.StatusCode, out
}

// waitQueueState polls /api/state until the queue is in the shape the caller
// describes, or fails with the reading it saw last.
func waitQueueState(t *testing.T, base string, what string, cond func(webStateSnapshot) bool) webStateSnapshot {
	t.Helper()
	deadline := time.Now().Add(webStateTestClockSkew)
	var last webStateSnapshot
	for {
		last = readState(t, base)
		if cond(last) {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s; last reading: queue_state=%q pending=%+v state=%q",
				what, last.QueueState, last.Pending, last.State)
		}
		time.Sleep(webQueueTestPoll) //stylegate:allow-sleep bounded poll for a side effect on the server's own goroutine
	}
}

// TestWebQueuePositionsAndCancelSemantics is the unit-level pin on the queue's
// own rules: arrival order becomes POSITION among the queued entries, a
// steering entry has no position and is NOT withdrawable, and a started entry
// stops being withdrawable.
func TestWebQueuePositionsAndCancelSemantics(t *testing.T) {
	var q webQueue
	a := q.enqueue(webStateQueued, "first")
	b := q.enqueue(webStateSteering, "steer")
	c := q.enqueue(webStateQueued, "second")

	got := q.entries()
	if len(got) != 3 {
		t.Fatalf("queue renders %d entries, want 3: %+v", len(got), got)
	}
	if got[0].State != webStateQueued || got[0].Position != 1 {
		t.Errorf("entry 1 = %+v, want a queued entry at position 1", got[0])
	}
	if got[1].State != webStateSteering || got[1].Position != 0 {
		t.Errorf("entry 2 = %+v, want a steering entry with no position", got[1])
	}
	if got[1].Cancellable {
		t.Errorf("a steering entry reports cancellable=true; withdrawing it is a different act from withdrawing a queued message")
	}
	if got[2].Position != 2 {
		t.Errorf("entry 3 = %+v, want position 2 (positions count QUEUED entries only)", got[2])
	}

	// A steering entry cannot be withdrawn through the queue route.
	if q.cancel(b.id) {
		t.Errorf("cancel(steering id %d) = true, want false", b.id)
	}
	// The queued one can be — and then the queue is shorter and RENUMBERED.
	if !q.cancel(a.id) {
		t.Fatalf("cancel(queued id %d) = false, want true", a.id)
	}
	after := q.entries()
	if len(after) != 2 {
		t.Fatalf("after withdrawing one of three, the queue renders %d entries, want 2: %+v", len(after), after)
	}
	if after[1].ID != c.id || after[1].Position != 1 {
		t.Errorf("the remaining queued entry = %+v, want id %d at position 1 after the withdrawal", after[1], c.id)
	}
	// A second withdrawal of the same id is refused rather than double-counted.
	if q.cancel(a.id) {
		t.Errorf("cancel of an already withdrawn id = true, want false")
	}
	// A started entry is the current turn, not a waiting message.
	q.markStarted(c)
	if q.cancel(c.id) {
		t.Errorf("cancel of a STARTED entry = true, want false")
	}
	if len(q.entries()) != 1 {
		t.Errorf("a started entry is still listed as waiting: %+v", q.entries())
	}
}

// TestWebQueueRegistrationReachesTheSnapshotAndTheStream proves the queue is
// carried by the surfaces that already existed — /api/state and the standing
// /api/events stream — rather than by a new polling channel (Rule 35).
func TestWebQueueRegistrationReachesTheSnapshotAndTheStream(t *testing.T) {
	_, httpSrv, _, _ := webQueueTurnServer(t)

	// The standing stream first, so the frames below are pushed, not asked for.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
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

	code, first := postJSON(t, httpSrv.URL, webChatQueuePath, map[string]string{"message": "waiting one"})
	if code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", webChatQueuePath, code)
	}
	if first["state"] != webStateQueued || first["position"].(float64) != 1 {
		t.Fatalf("first registration = %v, want state=%s position=1", first, webStateQueued)
	}
	_, second := postJSON(t, httpSrv.URL, webChatQueuePath, map[string]string{
		"message": "steer this", "placement": webPlacementSteering,
	})
	if second["state"] != webStateSteering || second["position"].(float64) != 0 {
		t.Fatalf("steering registration = %v, want state=%s position=0", second, webStateSteering)
	}
	_, third := postJSON(t, httpSrv.URL, webChatQueuePath, map[string]string{"message": "waiting two"})
	if third["position"].(float64) != 2 {
		t.Fatalf("third registration = %v, want position 2 (the second queued message)", third)
	}

	snap := readState(t, httpSrv.URL)
	if snap.QueueState != webStateQueued {
		t.Errorf("queue_state = %q with a queued message waiting, want %q", snap.QueueState, webStateQueued)
	}
	if len(snap.Pending) != 3 {
		t.Fatalf("pending = %+v, want three entries", snap.Pending)
	}
	if snap.Pending[1].State != webStateSteering || snap.Pending[1].Position != 0 {
		t.Errorf("pending[1] = %+v, want the steering entry with no position", snap.Pending[1])
	}
	firstID := int(first["id"].(float64))
	if snap.Pending[0].ID != firstID || !snap.Pending[0].Cancellable {
		t.Errorf("pending[0] = %+v, want id %d and cancellable", snap.Pending[0], firstID)
	}

	// Withdraw the first message; the queue shrinks and renumbers.
	code, cancelled := postJSON(t, httpSrv.URL, webChatQueueCancelPath, map[string]int{"id": firstID})
	if code != http.StatusOK {
		t.Fatalf("POST %s = %d, want 200", webChatQueueCancelPath, code)
	}
	if cancelled["ok"] != true {
		t.Fatalf("cancel = %v, want ok=true", cancelled)
	}
	snap = readState(t, httpSrv.URL)
	if len(snap.Pending) != 2 {
		t.Fatalf("after the withdrawal pending = %+v, want two entries", snap.Pending)
	}
	for _, e := range snap.Pending {
		if e.ID == firstID {
			t.Errorf("the withdrawn entry %d is still in the queue", firstID)
		}
	}
	if snap.Pending[1].Position != 1 {
		t.Errorf("pending[1] = %+v, want the queued message renumbered to position 1", snap.Pending[1])
	}

	// The frames carried the same document: one with a queued entry at position
	// 1, and one where the withdrawn id is gone.
	frames := collectStateFrames(t, res.Body)
	var sawQueued, sawWithdrawn bool
	for _, f := range frames {
		for _, e := range f.Pending {
			if e.State == webStateQueued && e.Position == 1 {
				sawQueued = true
			}
		}
		if f.QueueState == webStateQueued {
			gone := true
			for _, e := range f.Pending {
				if e.ID == firstID {
					gone = false
				}
			}
			if gone {
				sawWithdrawn = true
			}
		}
	}
	if !sawQueued {
		t.Errorf("no state frame carried a queued entry at position 1; frames=%+v", frames)
	}
	if !sawWithdrawn {
		t.Errorf("no state frame reported the queue after the withdrawal; frames=%+v", frames)
	}
}

// TestWebQueueWithdrawnMessageNeverRuns is the epic's minimal reproduction of
// the WITHDRAWAL: a real turn holds the lock inside a tool call, a second
// message is genuinely waiting on it, the operator withdraws it, and the turn
// it would have run never reaches the model.
//
// The evidence is the model's own request count: a withdrawn message that still
// ran would add a request, and the transcript would grow by its text.
func TestWebQueueWithdrawnMessageNeverRuns(t *testing.T) {
	ws, httpSrv, modelRequests, modelBodies := webQueueTurnServer(t)

	// TURN 1 — posted for real, and it parks inside the tool call.
	turnOne := make(chan struct{})
	go func() {
		defer close(turnOne)
		ws.handleChat(newStreamRecorder(), chatRequestForTest(t, "read the hostname file"))
	}()
	waitQueueState(t, httpSrv.URL, "the first turn to be park itself in a tool", func(s webStateSnapshot) bool {
		return s.State == webStateToolRunning
	})
	requestsDuringTurnOne := modelRequests()

	// TURN 2 — a second message arrives while turn 1 runs. Its request WAITS on
	// s.mu, and it is registered server-side so it has an id to withdraw.
	rec := newStreamRecorder()
	turnTwo := make(chan struct{})
	go func() {
		defer close(turnTwo)
		ws.handleChat(rec, chatRequestForTest(t, webQueueWithdrawnText))
	}()

	waiting := waitQueueState(t, httpSrv.URL, "the second message to appear in the queue", func(s webStateSnapshot) bool {
		return len(s.Pending) == 1 && s.Pending[0].State == webStateQueued
	})
	waitingID := waiting.Pending[0].ID
	if waiting.Pending[0].Position != 1 {
		t.Fatalf("the waiting message = %+v, want position 1", waiting.Pending[0])
	}

	// THE WITHDRAWAL, through the real mux.
	code, body := postJSON(t, httpSrv.URL, webChatQueueCancelPath, map[string]int{"id": waitingID})
	if code != http.StatusOK || body["ok"] != true {
		t.Fatalf("POST %s = %d %v, want 200 with ok=true", webChatQueueCancelPath, code, body)
	}

	// The waiting request learns it before turn 1 even ends — a withdrawn
	// message must not sit behind the very turn it no longer asks for.
	waitFor(t, webStateTestClockSkew, "the withdrawn request to answer its cancelled frame", func() bool {
		return sseContains(rec.body(), webEventCancelled)
	})
	if sseContains(rec.body(), webEventDelta) {
		t.Errorf("the withdrawn request streamed model output; body=%s", rec.body())
	}

	<-turnTwo
	<-turnOne

	// THE EVIDENCE, and why it is the BODY rather than a request COUNT: turn 1
	// legitimately makes a second round trip after its tool result, so a count
	// cannot separate "the withdrawn message ran" from "turn 1 finished". A
	// request body that does not contain the withdrawn text is the reading that
	// cannot be explained any other way.
	bodies := modelBodies()
	if len(bodies) == 0 {
		t.Fatalf("the mock model recorded no request bodies — the count assertion below would prove nothing")
	}
	if got := modelRequests(); got < requestsDuringTurnOne {
		t.Errorf("model requests went backwards: %d < %d", got, requestsDuringTurnOne)
	}
	for _, body := range modelBodies() {
		if strings.Contains(body, webQueueWithdrawnText) {
			t.Errorf("the withdrawn message reached the model in a request body")
		}
	}
	ws.mu.Lock()
	var transcript strings.Builder
	for _, m := range ws.history {
		transcript.WriteString(m.Content + "\n")
	}
	ws.mu.Unlock()
	if strings.Contains(transcript.String(), webQueueWithdrawnText) {
		t.Errorf("the withdrawn message reached the server-owned transcript: %q", transcript.String())
	}
	snap := readState(t, httpSrv.URL)
	if len(snap.Pending) != 0 || snap.QueueState != "" {
		t.Errorf("after the withdrawal the queue reads queue_state=%q pending=%+v, want empty", snap.QueueState, snap.Pending)
	}
}

// TestWebQueueCancelRefusesUnknownAndSteering pins the two refusals the route
// must give, and that a refusal is ANSWERED (200 with ok=false) rather than
// looking like success to the page's button.
func TestWebQueueCancelRefusesUnknownAndSteering(t *testing.T) {
	_, httpSrv, _, _ := webQueueTurnServer(t)

	_, steering := postJSON(t, httpSrv.URL, webChatQueuePath, map[string]string{
		"message": "addressed to the run", "placement": webPlacementSteering,
	})
	code, refused := postJSON(t, httpSrv.URL, webChatQueueCancelPath, map[string]int{"id": int(steering["id"].(float64))})
	if code != http.StatusOK || refused["ok"] != false {
		t.Errorf("cancelling a steering entry = %d %v, want 200 with ok=false", code, refused)
	}
	if got := readState(t, httpSrv.URL); len(got.Pending) != 1 {
		t.Errorf("a refused cancel removed the entry: pending=%+v", got.Pending)
	}

	code, unknown := postJSON(t, httpSrv.URL, webChatQueueCancelPath, map[string]int{"id": 9999})
	if code != http.StatusOK || unknown["ok"] != false {
		t.Errorf("cancelling an unknown id = %d %v, want 200 with ok=false", code, unknown)
	}

	// A body with no id is a 400 — the route names what it wants.
	code, _ = postJSON(t, httpSrv.URL, webChatQueueCancelPath, map[string]string{})
	if code != http.StatusBadRequest {
		t.Errorf("POST %s without an id = %d, want 400", webChatQueueCancelPath, code)
	}
	// GET is refused: the route changes the queue.
	res, err := http.Get(httpSrv.URL + webChatQueueCancelPath)
	if err != nil {
		t.Fatalf("GET %s: %v", webChatQueueCancelPath, err)
	}
	defer res.Body.Close() //stylegate:ignore the close of a test response has no assertion riding on it
	if res.StatusCode != http.StatusMethodNotAllowed {
		t.Errorf("GET %s = %d, want 405", webChatQueueCancelPath, res.StatusCode)
	}
}

// TestWebQueueAbandonedRegistrationStopsWaiting pins the ONE way an entry can
// leave the queue without a withdrawal: a registration whose page went away
// before it POSTed the turn.
//
// THE DEFECT THIS PINS (measured on the live proof, step 12): the page
// registers a mid-turn message when it is TYPED and POSTs the turn later from
// drainQueue. A tab that is closed or reloaded never sends that POST, and
// before this bound the server listed the entry forever — the badge read
// "排队中" over a queue nothing would ever run, which is exactly the false
// reading this epic exists to end.
//
// THE BOUND COUNTS IDLE TIME, which is the whole correctness of it: a message
// that waited out a 45-second tool call is the most legitimate entry in the
// queue, so the clock starts at the moment the last turn ENDED (noteTurnEnd),
// never at the registration instant.
func TestWebQueueAbandonedRegistrationStopsWaiting(t *testing.T) {
	age := int64(webQueueAbandonAfter / time.Second)
	now := int64(1_000_000)

	// While a turn is in flight the message waits however long it takes.
	var live webQueue
	live.enqueueAt(webStateQueued, "waiting out a long turn", now)
	for _, at := range []int64{now + age, now + 3*age} {
		if got := live.entriesFor(true, at); len(got) != 1 {
			t.Fatalf("entries with a turn in flight at +%ds = %+v, want the message still waiting (a long turn is not an abandonment)", at-now, got)
		}
	}
	// The turn ends; the idle clock starts THERE, not at registration — a
	// message that waited three bounds' worth of turn is still a real message.
	live.noteTurnEnd(now + 3*age)
	if got := live.entriesFor(false, now+3*age+1); len(got) != 1 {
		t.Fatalf("entries one second after a long turn ended = %+v, want the message still waiting", got)
	}
	// Nobody claims it for a full bound of IDLE time: the page that registered
	// it is gone, and reporting it as waiting is the false reading this pins.
	if got := live.entriesFor(false, now+4*age); len(got) != 0 {
		t.Fatalf("entries after a bound of idle time = %+v, want none: a registration nothing claims while the server is idle is a page that went away, not a waiting message", got)
	}
	// ...and it does not come back on the next read (the renderer RECLAIMS).
	if got := live.entriesFor(false, now+4*age); len(got) != 0 {
		t.Fatalf("the abandoned entry came back on a second read = %+v (the renderer must reclaim what it drops)", got)
	}

	// A registration made while idle gets its own full bound: still a message a
	// second in, litter only past the bound.
	var idle webQueue
	idle.enqueueAt(webStateQueued, "typed then abandoned", now)
	if got := idle.entriesFor(false, now+1); len(got) != 1 {
		t.Fatalf("entries one second after registration = %+v, want the entry still waiting", got)
	}
	if got := idle.entriesFor(false, now+age); len(got) != 0 {
		t.Fatalf("entries a bound after registration = %+v, want none", got)
	}

	// A live registration is unaffected by another entry aging out, and it is
	// numbered from 1 again.
	var mixed webQueue
	mixed.enqueueAt(webStateQueued, "aged out", now)
	second := mixed.enqueueAt(webStateQueued, "fresh", now+age)
	got := mixed.entriesFor(false, now+age)
	if len(got) != 1 || got[0].ID != second.id || got[0].Position != 1 {
		t.Fatalf("after an aged entry dropped out, entries = %+v, want only id %d at position 1", got, second.id)
	}
}

// TestWebQueuePageCarriesTheTextNotJustTheStyle pins the PAGE half: the status
// words exist as TEXT (the pre-epic page distinguished the two placements with a
// border style and called that "cosmetic only"), the routes are the server's
// spellings, and the badge renders the server's queue view rather than deriving
// one from its own array.
func TestWebQueuePageCarriesTheTextNotJustTheStyle(t *testing.T) {
	ws := &webServer{addr: ":0"}
	rec := httptest.NewRecorder()
	ws.handleRoot(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()

	for _, want := range []string{
		// The page's own spellings of the two status sentences, pinned as TEXT
		// (the page is a Go raw string, so these cannot be imported — Rule 13's
		// pair-pinning rule, the same shape the existing badge test uses).
		"排队中 · 当前回合结束后发送",
		"已插入当前回合",
		"撤销",
		"已撤销 · 未发送",
		"const API_QUEUE='" + webChatQueuePath + "'",
		"const API_QUEUE_CANCEL='" + webChatQueueCancelPath + "'",
		// The page's state spellings are ALIASES of its placement pair, so the
		// wire's two fields cannot drift into two vocabularies (Rule 14).
		"const ST_QUEUED=PLACEMENT_QUEUED;",
		"const ST_STEERING=PLACEMENT_STEERING;",
		"const PLACEMENT_QUEUED='" + webStateQueued + "', PLACEMENT_STEERING='" + webStateSteering + "';",
		"const EV_CANCELLED='" + webEventCancelled + "'",
		"function renderQueueStatus(",
		"function withdrawQueued(",
		"function applyQueue(",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("the page does not carry %q — the queue's text/route and the server would disagree", want)
		}
	}
	// The words must be RENDERED, not merely defined: the status line is built
	// with textContent from those constants.
	if !strings.Contains(page, "line.textContent=QUEUED_LABEL") {
		t.Errorf("the page defines the queued wording but never renders it into the entry's status line")
	}
}

// sseContains reports whether an SSE body carries a frame with the given event
// name.
func sseContains(body, event string) bool {
	_, ok := sseDataOf(body, event)
	return ok
}
