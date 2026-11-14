package main

// chat_notice_turn_busy_test.go — the BUSY-SIDE guard for epic 01a0eeea
// (flagship 5): a job-notice wake arriving while a turn is IN FLIGHT must not
// interrupt that turn, must not be injected between a tool result and the
// assistant message, and must not rewrite any past message. The notice waits in
// the inbox and becomes the NEXT turn (Option B — argued in chat_notice_turn.go).
//
// WHY IT IS A TEST AND NOT PROSE. The invariant is about ORDER in the ONE
// conversation, and order is exactly what a "looks right" reading cannot check.
// The case drives the real `runChatWithInput` over the real agent, with the model
// server HOLDING its first response open mid-stream, so the notice provably lands
// while the turn is still running — a synchronization point, never a sleep race.
//
// THE READINGS, all from the WIRE (never the screen):
//  1. NO SECOND REQUEST WHILE HELD — the turn was not abandoned and no other turn
//     was minted in flight;
//  2. ROLE ALTERNATION — no request body contains two adjacent assistant
//     messages, which is the wire-level statement of "the notice was not wedged
//     between a tool result and the assistant's answer";
//  3. PREFIX FROZEN — the operator's own line appears exactly ONCE in the minted
//     turn's request, byte-identical: the past was not rewritten or duplicated.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/tools"
)

const (
	// chatNoticeBusyPoll bounds a WAIT (never a race): every check returns the
	// instant the state it names is observable.
	chatNoticeBusyPoll = 25 * time.Millisecond
	// chatNoticeBusyTimeout bounds every wait in this file. It is a test deadline
	// for a signal that must arrive on its own — never a load generator (Rule 27).
	chatNoticeBusyTimeout = 30 * time.Second
	// chatNoticeBusyLine is the operator's own message: the prefix that must come
	// back byte-identical in every later request.
	chatNoticeBusyLine = "the turn in flight must not be disturbed"
	// chatNoticeBusyJobCommand is the job the notice is raised by.
	chatNoticeBusyJobCommand = "printf chat-notice-busy-job"
	// chatNoticeBusyRequests is how many responses the server is scripted with:
	// the operator's turn, and the minted notice turn.
	chatNoticeBusyRequests = 2
	// chatNoticeMintedMark is the phrase only the AGENT composes
	// (agent.ChatJobNoticeOpenLine). Nothing the operator typed can produce it,
	// which is what makes it evidence that the CONSOLE opened the turn.
	chatNoticeMintedMark = "Background job "
	// chatNoticeBodyQuote caps how much of a failing request body is printed: a
	// chat request is large and the useful part is its message list.
	chatNoticeBodyQuote = 1500
)

// chatNoticeBusyServer is a minimal model server with ONE hold: its first
// response is written and then left open until release, so a turn can be pinned
// in flight for as long as a case needs. It is the same shape
// chat_boundary_test.go's inline server uses, extended with the hold.
type chatNoticeBusyServer struct {
	mu     sync.Mutex
	bodies []string
	// releaseOnce guards the ONE release: a second call is a no-op, so the case
	// cannot unblock the hold twice (and cannot panic a closed channel).
	releaseOnce sync.Once
	released    chan struct{} // closed when release() runs
}

func newChatNoticeBusyServer(t *testing.T) (*chatNoticeBusyServer, *httptest.Server) {
	t.Helper()
	s := &chatNoticeBusyServer{released: make(chan struct{})}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a test request body; a short read is acceptable
		s.mu.Lock()
		s.bodies = append(s.bodies, string(body))
		n := len(s.bodies)
		s.mu.Unlock()
		if n == 1 {
			// The FIRST response is held: headers first, then the park. Nothing
			// of the answer exists until release, so "the turn is still running"
			// is a fact of the server rather than of a clock.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			select {
			case <-s.released:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		// Both answers exceed the chat path's minimum final-answer length, so the
		// turn ends on the model's own answer rather than on a continuation round
		// — the request count this case reads is then about the notice, not about
		// the harness's degenerate-round handling.
		textSSE(w, "answer "+itoa(n)+" — a complete sentence that the chat turn accepts as its final answer.")
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

func (s *chatNoticeBusyServer) release() { s.releaseOnce.Do(func() { close(s.released) }) }
func (s *chatNoticeBusyServer) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bodies)
}
func (s *chatNoticeBusyServer) body(n int) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 1 || n > len(s.bodies) {
		return ""
	}
	return s.bodies[n-1]
}

// newChatNoticeClient builds the agent's model client pointed at the case's own
// server (the boundary harness's shape, with its client construction).
func newChatNoticeClient(srv *httptest.Server) *deepseek.Client {
	return &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()}
}

// claudeMessageRoles decodes a request body's message roles, in order.
func claudeMessageRoles(t *testing.T, body string) []string {
	t.Helper()
	var req deepseek.ClaudeRequest
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	roles := make([]string, 0, len(req.Messages))
	for _, m := range req.Messages {
		roles = append(roles, m.Role)
	}
	return roles
}

// TestChatNoticeTurn_NoticeDuringATurnDoesNotInterruptOrRewrite is the busy-side
// acceptance.
func TestChatNoticeTurn_NoticeDuringATurnDoesNotInterruptOrRewrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	t.Setenv("GOPI_MEMORY_DB", "")

	srvMock, srv := newChatNoticeBusyServer(t)
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })

	sl, err := agent.NewSessionLogger(chatBoundaryTestModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened

	ag := &agent.Agent{
		Client:     newChatNoticeClient(srv),
		Ctx:        &agent.Context{},
		Model:      chatBoundaryTestModel,
		SessionLog: sl,
	}
	ag.PersistChatSession = true
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	tools.ResetJobs()
	takeJobNoticesForTest()

	// THE INPUT IS A PIPE, NOT A FINISHED SCRIPT, and that is load-bearing: a
	// scripted reader hands the loop an EOF the moment its one line is consumed,
	// and an EOF is a line the loop already has — so the operator's own input
	// would win the boundary and the session would end before the notice ever
	// became a turn (correct behaviour, wrong fixture). The pipe stays open so the
	// only thing that can end the wait is the notice; the write end is closed at
	// the very end, which is the case's own /quit.
	pr, pw := io.Pipe()
	// The write rides its own goroutine because io.Pipe is UNBUFFERED: a write
	// from this goroutine would block until the reader arrived, and the reader is
	// started on the next line.
	go func() { _, _ = pw.Write([]byte(chatNoticeBusyLine + "\n")) }() //stylegate:ignore writes the busy-notice line into the pty from a goroutine; a write error only means the reader stopped, which is asserted separately
	in, closeIn := newChatInputCooked(pr, io.Discard)
	done := make(chan int, 1)
	go func() { done <- runChatWithInput([]string{"--no-editor"}, in) }()

	// THE TURN IS PROVABLY IN FLIGHT: the server has the body and is parked.
	waitForChatCount(t, srvMock, 1)

	// THE NOTICE, raised through the PRODUCTION pair: the reaper appends and then
	// signals the edge. A hand-rolled channel send would test nothing.
	if _, err := tools.JobStart(chatNoticeBusyJobCommand); err != nil {
		t.Fatalf("JobStart: %v", err)
	}
	waitForNoticePending(t)

	// (1) NO SECOND REQUEST WHILE THE TURN IS HELD — the turn was not abandoned,
	// and no second turn was minted in flight (the busy case queues the notice in
	// the inbox instead; a turn here would be the mid-turn injection this epic
	// forbids).
	if got := srvMock.count(); got != 1 {
		t.Fatalf("the model served %d request(s) while the first turn was held — the notice minted a turn IN FLIGHT, which the busy case must never do", got)
	}

	// RELEASE: the turn finishes normally and the boundary below runs.
	srvMock.release()

	// The minted notice turn is the SECOND request, and its message is the
	// agent's own notice formatter.
	waitForChatCount(t, srvMock, chatNoticeBusyRequests)
	body := srvMock.body(chatNoticeBusyRequests)

	// (2) ROLE ALTERNATION on the wire: no adjacent assistant messages anywhere in
	// the request, which is what "the notice was not wedged between a tool result
	// and the assistant's answer" means once it is a message list.
	assertClaudeRolesAlternate(t, body)
	// (3) THE PREFIX IS FROZEN, byte-identical and exactly once.
	if !strings.Contains(body, chatNoticeMintedMark) {
		t.Fatalf("the second request does not carry the notice message (%q) — the notice never became a turn:\n%s", chatNoticeMintedMark, truncateForTest(body, chatNoticeBodyQuote))
	}
	if n := strings.Count(body, chatNoticeBusyLine); n != 1 {
		t.Fatalf("the operator's line appears %d time(s) in the notice turn's request, want exactly 1 — the wait must APPEND a turn, never rewrite or duplicate the past:\n%s",
			n, truncateForTest(body, chatNoticeBodyQuote))
	}

	// END THE SESSION at the input layer's own boundary: closing the read end of
	// the pipe is the cooked reader's EOF, which is what /quit is on this face.
	closeIn()
	_ = pw.Close() //stylegate:ignore the case's own end-of-input: the write end has nothing left to say
	select {
	case <-done:
	case <-time.After(chatNoticeBusyTimeout):
		t.Fatal("runChatWithInput did not return after the input closed — the session did not end cleanly")
	}
}

// assertClaudeRolesAlternate decodes the request's message list and fails if two
// assistant messages are adjacent (the wire's own statement of the invariant).
// The decode is tolerant by design: it only needs the roles.
func assertClaudeRolesAlternate(t *testing.T, body string) {
	t.Helper()
	roles := claudeMessageRoles(t, body)
	for i := 1; i < len(roles); i++ {
		if roles[i] == "assistant" && roles[i-1] == "assistant" {
			t.Fatalf("the request's message list has two adjacent assistant messages at %d (roles: %v) — a notice WAS injected between a tool result and the assistant's answer", i, roles)
		}
	}
}

// waitForChatCount waits (bounded) for the server to have served n requests.
func waitForChatCount(t *testing.T, s *chatNoticeBusyServer, n int) {
	t.Helper()
	deadline := time.Now().Add(chatNoticeBusyTimeout)
	for s.count() < n {
		if time.Now().After(deadline) {
			t.Fatalf("the model served %d request(s), want %d within %s", s.count(), n, chatNoticeBusyTimeout)
		}
		time.Sleep(chatNoticeBusyPoll) //stylegate:allow-sleep bounded poll of the fixture server's request count, which exposes no completion channel to wait on
	}
}

// waitForNoticePending waits (bounded) until the job's completion notice is in
// the inbox — the LEVEL the minted turn is decided by.
func waitForNoticePending(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(chatNoticeBusyTimeout)
	for !tools.JobNoticePending() {
		if time.Now().After(deadline) {
			t.Fatal("no completion notice landed in the inbox — the reaper never appended, so the case has nothing to be about")
		}
		time.Sleep(chatNoticeBusyPoll) //stylegate:allow-sleep bounded poll of the job-notice inbox level, which offers no completion channel to wait on
	}
}

// takeJobNoticesForTest drains the inbox so the case starts from a known level.
func takeJobNoticesForTest() { tools.TakeJobNotices() }
