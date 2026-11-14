package agent

// session_inbox_test.go — the agent-side half of `gopi notify --session <id>
// --message <text>`.
//
// TWO CLAIMS, and they need different drivers:
//
//  1. THE ROUND BOUNDARY INJECTS IT. An in-process Agent with a real
//     SessionLogger, a real inbox file on disk and a real model round: the
//     message must reach the WIRE (the request body the mock server receives)
//     and the SESSION RECORD (the JSONL line a later reader — and a resumed
//     session — sees). A test that asserted only the wire would pass with the
//     message absent from the record, which is the state in which a resumed
//     session re-injects it or loses it entirely.
//
//  2. THE TAKE POINT STAYS UNIQUE. A source-level gate, because "there is
//     exactly one reader" is a claim about the tree, not about one run: a
//     second `TakeJobNotices()` call site added elsewhere (or a second queue
//     grown beside it) is exactly the drift the constraint forbids, and no
//     behavioural case can see code that has not run yet.

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionlayout"
	"github.com/nickhuang99/gopi/internal/tools"
)

// sessionInboxTestMessage is the text delivered in the in-process case. It is
// distinctive so that "it reached the wire" cannot be satisfied by the turn's
// own input or by the system prompt.
const sessionInboxTestMessage = "NOTIFY-INPROC-4c1e: delivered from outside this process"

// sessionInboxInjectionMarker is the HARNESS STATEMENT that precedes an
// injected notice on the wire ("[HARNESS]"-prefixed). It is the right subject
// for "was it injected again": the delivered TEXT legitimately appears in a
// replayed conversation, while a second statement is exactly the duplicate
// instruction this feature must not produce.
var sessionInboxInjectionMarker = harnessMessagePrefix + "An external process sent this session a message:"

// TestSessionInboxReachesTheWireAndTheSessionRecord is the round-boundary
// claim, on the chat turn's own path (Agent.ChatOps).
func TestSessionInboxReachesTheWireAndTheSessionRecord(t *testing.T) {
	var bodies []string
	var calls int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a request body the mock itself served
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"acknowledged"}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	// A PERSISTENT chat (one session log across turns, closed by the carrier):
	// that is the shape `gopi chat` runs, and it is what keeps the log open for
	// the record read below (a per-turn log is closed by ChatOps itself).
	ag.PersistChatSession = true
	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() {
		if cerr := sl.Close(); cerr != nil {
			t.Errorf("close session log: %v", cerr)
		}
	}()
	ag.SessionLog = sl

	// The delivery, made the way the CLI makes it: through internal/tools, into
	// the session's own directory — the directory the run is writing its record
	// in, which is what the agent resolves the inbox from.
	dir := sessionlayout.RecordContainer(sl.Path())
	del, err := tools.SessionInboxDeliver(dir, sl.SessionID(), sessionInboxTestMessage, "")
	if err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}
	if del.Path != tools.SessionInboxPath(dir) {
		t.Fatalf("delivery landed at %s, want the session dir's inbox %s", del.Path, tools.SessionInboxPath(dir))
	}

	if _, err := ag.ChatOps("the operator's own turn", nil); err != nil {
		t.Fatalf("ChatOps: %v", err)
	}

	// THE WIRE: the model read the message in THIS turn's request.
	if len(bodies) == 0 {
		t.Fatal("no request reached the mock — nothing was sent")
	}
	onWire := false
	for _, b := range bodies {
		if strings.Contains(b, sessionInboxTestMessage) {
			onWire = true
		}
	}
	if !onWire {
		t.Fatalf("no request carried the delivered message; bodies:\n%s", strings.Join(bodies, "\n---\n"))
	}

	// THE RECORD: the session JSONL carries it as a user turn, so a resumed
	// session replays the same fact instead of losing it.
	raw, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read session record: %v", err)
	}
	got := false
	for _, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var ev map[string]any
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("session record line is not one JSON object (a line loop would accept this): %v\n%s", err, line)
		}
		if ev["type"] != "user" {
			continue
		}
		m, _ := ev["message"].(map[string]any)
		if m == nil {
			continue
		}
		if c, _ := m["content"].(string); strings.Contains(c, sessionInboxTestMessage) {
			got = true
		}
	}
	if !got {
		t.Errorf("the session record carries no user event with the delivered message:\n%s", raw)
	}
}

// TestSessionInboxIsDeliveredOncePerTurn pins the read semantics from the
// agent's side: the message appears in the request of the turn that loaded it
// and NOT in the next turn's request (the history replay of the session is what
// carries it thereafter — the agent's own conversation object holds it).
//
// The second turn is a NEW turn on the SAME agent (a persistent chat), which is
// the shape a delivery must not survive twice: a re-injected instruction like
// "run the tests" would run twice.
func TestSessionInboxIsDeliveredOncePerTurn(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a request body the mock itself served
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"acknowledged"}, "end_turn")
	}))
	defer server.Close()

	home := t.TempDir()
	t.Setenv("GOPI_HOME", home)
	ag := chatOpsAgent(t, server)
	// A PERSISTENT chat: ONE session log spans both turns, which is the shape
	// the dedup property is about (with a per-turn log the two turns would be
	// two sessions and the question would not arise).
	ag.PersistChatSession = true
	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() {
		if cerr := sl.Close(); cerr != nil {
			t.Errorf("close session log: %v", cerr)
		}
	}()
	ag.SessionLog = sl
	dir := sessionlayout.RecordContainer(sl.Path())
	if _, err := tools.SessionInboxDeliver(dir, sl.SessionID(), sessionInboxTestMessage, ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}

	if _, err := ag.ChatOps("turn one", nil); err != nil {
		t.Fatalf("first ChatOps: %v", err)
	}
	first := len(bodies)
	if first == 0 {
		t.Fatal("the first turn issued no request")
	}
	if !strings.Contains(bodies[0], sessionInboxTestMessage) {
		t.Errorf("the first turn's request did not carry the delivery:\n%s", bodies[0])
	}
	// The carrier's own seeding (cli_chat.go): the accumulated conversation of
	// the previous turn is what the next turn is built from — ONE history, shared.
	history := append([]deepseek.Message{}, ag.Ctx.Messages...)
	if _, err := ag.ChatOps("turn two", history); err != nil {
		t.Fatalf("second ChatOps: %v", err)
	}
	if len(bodies) <= first {
		t.Fatal("the second turn issued no request")
	}
	// THE DELIVERY IS NOT RE-INJECTED, and that is the whole assertion: the
	// second turn's request carries the message EXACTLY ONCE — the copy the
	// session's own conversation holds (the first turn's history) — never a
	// second injection from the inbox. The observable is the HARNESS STATEMENT,
	// which is what a re-injection would add: the raw message text legitimately
	// appears once in the replayed history, and a second occurrence is the
	// duplicate instruction ("run the tests" running twice) this feature must
	// never produce.
	second := bodies[first]
	if got := strings.Count(second, sessionInboxInjectionMarker); got != 1 {
		t.Errorf("the second turn's request carries the injected notice %d time(s), want exactly the one in the replayed history:\n%s", got, second)
	}
	inbox, err := os.ReadFile(tools.SessionInboxPath(dir))
	if err != nil {
		t.Fatalf("read inbox: %v", err)
	}
	if got := strings.Count(string(inbox), sessionInboxTestMessage); got != 1 {
		t.Errorf("the inbox holds the message %d time(s) — a delivery is one record", got)
	}
	if !strings.Contains(string(inbox), `"kind":"delivered"`) {
		t.Errorf("the inbox carries no delivered marker, so the message would be merged again on the next start:\n%s", inbox)
	}
}

// TestSessionInboxReachesTheNoToolsChatTurn is the boundary a DEFAULT
// `gopi chat` takes: no `--tools`, so the turn runs `Agent.Chat`, not
// `Agent.ChatOps`. Wiring only ChatOps would have left the feature existent and
// unreachable on the operator's ordinary console — the failure Rule 31 is for —
// so this is the case that keeps that arm honest.
func TestSessionInboxReachesTheNoToolsChatTurn(t *testing.T) {
	var bodies []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of a request body the mock itself served
		bodies = append(bodies, string(body))
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"acknowledged"}, "end_turn")
	}))
	defer server.Close()

	ag := chatOpsAgent(t, server)
	ag.PersistChatSession = true
	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer func() {
		if cerr := sl.Close(); cerr != nil {
			t.Errorf("close session log: %v", cerr)
		}
	}()
	ag.SessionLog = sl
	dir := sessionlayout.RecordContainer(sl.Path())
	if _, err := tools.SessionInboxDeliver(dir, sl.SessionID(), sessionInboxTestMessage, ""); err != nil {
		t.Fatalf("SessionInboxDeliver: %v", err)
	}

	if _, err := ag.Chat("the operator's own turn", nil); err != nil {
		t.Fatalf("Chat (no tools): %v", err)
	}
	if len(bodies) == 0 {
		t.Fatal("the no-tools turn issued no request")
	}
	if !strings.Contains(bodies[0], sessionInboxTestMessage) {
		t.Errorf("the no-tools chat turn's request did not carry the delivery — a default `gopi chat` would never see it:\n%s", bodies[0])
	}
}

// TestSessionInboxLoadHasOneTakePoint is constraint ① as a gate over the tree:
// `TakeJobNotices` is DEFINED once (internal/tools/job.go) and CALLED only from
// the round boundaries that first merge the durable inbox into it. A second
// reader — a second queue, a second drain, a side channel — fails here by name.
//
// The gate reads PRODUCTION sources only (`_test.go` excluded): a test is
// allowed to drain the queue to get a clean fixture, and calling that a
// violation would make the gate a nuisance instead of a guard.
func TestSessionInboxLoadHasOneTakePoint(t *testing.T) {
	// The definition site, and the call sites this feature is allowed to have.
	// Spelled as data so a change is a deliberate edit here, in the open.
	const (
		takePointName     = "TakeJobNotices("
		definitionPath    = "internal/tools/job.go"
		loadCallName      = "loadSessionInbox()"
		roundBoundaryFile = "internal/agent/chat_ops.go"
		taskLoopFooFile   = "internal/agent/loop.go"
	)
	roots := []string{"../../internal", "../../cmd"}

	type site struct{ file string }
	var defs []string
	calls := map[string]int{}
	loadCalls := map[string]int{}

	walkErr := walkGoSources(t, roots, func(path, src string) {
		for _, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") {
				continue // a comment naming the take point is not a take
			}
			if strings.Contains(trimmed, "func "+takePointName) {
				defs = append(defs, path)
			} else if strings.Contains(line, takePointName) {
				calls[path]++
			}
			if strings.Contains(trimmed, loadCallName) {
				loadCalls[path]++
			}
		}
	})
	if walkErr != nil {
		t.Fatalf("walk sources: %v", walkErr)
	}

	if len(defs) != 1 {
		t.Fatalf("%s is defined %d time(s) (%v), want exactly 1 — one queue, one definition", takePointName, len(defs), defs)
	}
	if defs[0] != definitionPath {
		t.Errorf("the take point is defined in %s, want %s", defs[0], definitionPath)
	}
	if len(calls) != 2 || calls[roundBoundaryFile] == 0 || calls[taskLoopFooFile] == 0 {
		t.Errorf("call sites = %v, want only %s and %s — an external message must leave through the SAME take point the job notices use",
			calls, roundBoundaryFile, taskLoopFooFile)
	}
	// EVERY take is preceded, in the SAME file, by the load that merges the
	// durable inbox into that queue: a boundary that drained without loading
	// would deliver the message one round late at best, never at worst.
	for file := range calls {
		if loadCalls[file] == 0 {
			t.Errorf("%s drains the inbox but never calls %s — the durable half would never reach the model on that path", file, loadCallName)
		}
	}
}

// walkGoSources visits every production .go file (tests excluded) under the
// given roots, handing the caller its path and contents relative to the module
// root.
func walkGoSources(t *testing.T, roots []string, visit func(path, src string)) error {
	t.Helper()
	for _, root := range roots {
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			raw, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			clean := filepath.ToSlash(filepath.Clean(strings.TrimPrefix(path, "../../")))
			visit(clean, string(raw))
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
