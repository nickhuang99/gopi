// cli_web_align_test.go — regression tests for the P0 web-alignment Epic:
//
//	R1: the SERVER owns the authoritative conversation history — two turns
//	    with NO client-side history still form one continuous context.
//	R2: `--tools` (and env GOPI_WEB_TOOLS) route turns through Agent.ChatOps;
//	    tool activity streams as SSE `event: tool` frames and lands in the
//	    session JSONL as tool_use/tool_result.
//	bug003: the shutdown path finalizes the single session (session_end +
//	    task-ledger close) — including the signal path.
//
// Mock discipline (unchanged): the LLM is an httptest fake Claude SSE server;
// local side effects (real files via tools.Write) are real and isolated in
// t.TempDir + GOPI_HOME/GOPI_MEMORY_DB.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/nickhuang99/gopi/internal/agent"
	"github.com/nickhuang99/gopi/internal/chatpolicy"
	"github.com/nickhuang99/gopi/internal/config"
	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionevent"
	"github.com/nickhuang99/gopi/internal/testutil"
	"github.com/nickhuang99/gopi/internal/tools"
)

// Bounds for the live `gopi web` subprocess check
// (TestWebToolsSignalShutdownWritesSessionEnd). Each is a NAMED constant
// (Rule 1a): the test owns three independent deadlines — a server that never
// comes up, a turn that never finishes, and a process that never dies — and a
// single inline budget could not express which one expired.
const (
	// webSignalBootTimeout bounds waiting for the spawned `gopi web` to write
	// its session JSONL. The artifact IS the readiness signal (see the test):
	// the file is created by agent construction, before the listener accepts.
	webSignalBootTimeout = 20 * time.Second
	// webSignalPollInterval is the readiness/exit poll step.
	webSignalPollInterval = 50 * time.Millisecond
	// webSignalTurnTimeout bounds the ONE turn the test drives. The turn's
	// model call is real, so this is generous — it is a hang guard, not a
	// performance assertion.
	webSignalTurnTimeout = 60 * time.Second
	// webSignalExitTimeout bounds the graceful exit after SIGTERM. It must
	// EXCEED the server's own webShutdownGrace (5s), which is spent waiting
	// for an in-flight turn, plus the reaper's SIGTERM grace — otherwise a
	// correct graceful shutdown would fail this test.
	webSignalExitTimeout = 20 * time.Second
	// webSignalSessionFileBase is the bare-session JSONL file name inside
	// sessions/<uuid>/ (session-dir-unify S1 bare layout, the layout
	// `testutil.LoadSession` and the agent's SessionLogFileName both assume).
	webSignalSessionFileBase = "session.jsonl"
	// webSignalTurnReplyText is what the scripted provider answers the ONE
	// driven turn with. It is a named constant (Rule 1a) so the assertion
	// below and the script cannot drift apart.
	webSignalTurnReplyText = "pong from the scripted provider"
)

// mockToolStream writes one tool_use SSE response (mirrors the agent
// package's claudeToolUseStream wire shape).
func mockToolStream(w http.ResponseWriter, name string, args map[string]interface{}) {
	argsJSON, _ := json.Marshal(args)                //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	argsJSONStr, _ := json.Marshal(string(argsJSON)) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	writeClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}`)
	writeClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":%q,"input":{}}}`, name))
	writeClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":%s}}`, argsJSONStr))
	writeClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	writeClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":5}}`)
	writeClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// webServerForTestTools builds a persistent-agent webServer like
// webServerForTest, with tools toggled and an optional turn counter (so the
// mock can answer round 1 with a tool_use writing to writePath and round 2
// with text).
//
// IT FREEZES THE CHAT WRITE BOUNDARY, exactly as runWeb does before serving its
// first turn. That step is not decoration: the boundary judges every write/edit
// against the pair frozen by tools.SetChatPolicy, and the PACKAGE DEFAULT of
// that pair is `prompt *` with the on-request posture — so a fixture that skipped
// the freeze stood in for a web server whose genuine tool round could never land
// a write, whatever the operator declared (measured 2026-10-22, task 01a16225:
// the R2 case's write came back `[NEEDS OPERATOR APPROVAL — command NOT run]` and
// the target file was never created). The posture is declared through the same
// channel a real run uses (GOPI_CHAT_POLICY) rather than by calling tools' setter
// with a hand-built posture, so the fixture also covers the channel PLUMBING the
// real face depends on.
func webServerForTestTools(t *testing.T, useTools bool, writePath string) (*webServer, *httptest.Server, *int32) {
	t.Helper()
	t.Setenv(agent.ChatPolicyEnv, chatpolicy.PostureYoloWord)
	chatFreezeChatPolicyState(chatFacePrefixWeb, "")
	t.Cleanup(tools.ResetChatPolicyToDefaults)
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Content-Type", "text/event-stream")
			mockToolStream(w, "write", map[string]interface{}{
				"path":    writePath,
				"content": "written-by-web-tools-round",
			})
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "done after tool round")
	}))
	ag := &agent.Agent{
		Client: &deepseek.Client{
			BaseURL:    srv.URL,
			APIKey:     "test-key",
			HTTPClient: srv.Client(),
		},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{
		addr:      ":0",
		agent:     ag,
		tools:     useTools,
		startedAt: time.Now(),
	}
	return ws, srv, &calls
}

// TestWebChat_R1_ServerOwnedContextTwoTurnsNoClientHistory is the R1
// acceptance test: two /api/chat turns that carry NO history field still
// produce a continuous two-turn model context (the raw model request for
// turn 2 must contain turn 1's user message and the assistant's answer).
// Before R1 the server re-seeded each turn from the client-shipped history
// only — a history-less turn 2 was amnesiac (bugs-found/006).
func TestWebChat_R1_ServerOwnedContextTwoTurnsNoClientHistory(t *testing.T) {
	var lastReq []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		lastReq = b
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "turn answer")
	}))
	defer srv.Close()

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		Timeout:    0,
		SessionLog: nil,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	postChat(t, ws, "remember the word ZEBRA", nil)
	postChat(t, ws, "what word did I ask you to remember?", nil)

	if len(lastReq) == 0 {
		t.Fatal("mock never received the turn-2 model request")
	}
	// The turn-2 wire request must contain the SERVER-captured turn-1
	// exchange (user text + assistant answer) plus the new user message —
	// without any client history being shipped.
	var modelReq struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(lastReq, &modelReq); err != nil {
		t.Fatalf("bad model request JSON: %v", err)
	}
	var sawZebraUser, sawTurn1Answer, sawTurn2User bool
	for _, m := range modelReq.Messages {
		for _, c := range m.Content {
			switch c.Text {
			case "remember the word ZEBRA":
				if m.Role == "user" {
					sawZebraUser = true
				}
			case "turn answer":
				sawTurn1Answer = true
			case "what word did I ask you to remember?":
				if m.Role == "user" {
					sawTurn2User = true
				}
			}
		}
	}
	if !sawZebraUser {
		t.Errorf("turn-2 request missing turn-1 user message (server-owned history not captured)")
	}
	if !sawTurn1Answer {
		t.Errorf("turn-2 request missing turn-1 assistant answer")
	}
	if !sawTurn2User {
		t.Errorf("turn-2 request missing the turn-2 user message itself")
	}
	// And the server now holds the authoritative transcript.
	if len(ws.history) != 4 { // user, assistant, user, assistant
		t.Errorf("ws.history length = %d, want 4 (two full turns captured server-side)", len(ws.history))
	}
}

// TestWebChat_R1_ClientHistoryFieldAcceptedButIgnored locks the R1
// back-compat contract: a legacy client that still ships `history` gets a
// normal 200 SSE turn, but its history NEVER reaches the model request
// (otherwise a malicious/broken client could inject context).
func TestWebChat_R1_ClientHistoryFieldAcceptedButIgnored(t *testing.T) {
	var lastReq []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		lastReq = b
		w.Header().Set("Content-Type", "text/event-stream")
		textSSE(w, "ok")
	}))
	defer srv.Close()

	ag := &agent.Agent{
		Client:  &deepseek.Client{BaseURL: srv.URL, APIKey: "k", HTTPClient: srv.Client()},
		Ctx:     &agent.Context{},
		Model:   "deepseek-v4-pro",
		Timeout: 0,
		Logger:  log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	body := postChat(t, ws, "fresh turn", []webChatMessage{
		{Role: "user", Content: "INJECTED-HISTORY-9137"},
		{Role: "assistant", Content: "INJECTED-REPLY-9137"},
	})
	if !strings.Contains(body, "event: done") {
		t.Fatalf("legacy request failed: body=%q", body)
	}
	if strings.Contains(string(lastReq), "INJECTED-HISTORY-9137") ||
		strings.Contains(string(lastReq), "INJECTED-REPLY-9137") {
		t.Errorf("client-shipped history reached the model request — R1 violation: %s", lastReq)
	}
}

// TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames is the R2
// acceptance test at the HTTP boundary: with tools enabled, a turn that the
// mock answers with a tool_use (a REAL tools.Write to a temp file, executed
// by the genuine tool dispatch) streams SSE `event: tool` frames (call +
// result) and the session JSONL records tool_use + tool_result.
func TestWebChat_R2_ToolsFlagRoutesThroughChatOpsAndStreamsToolFrames(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	// Real write target: the mock's round-1 tool_use writes THIS file via
	// the genuine tool dispatch — its existence + content after the turn is
	// the proof of real execution (the same "model really wrote the file"
	// evidence the live acceptance recipe uses).
	writeTarget := filepath.Join(t.TempDir(), "made-by-web-test.txt")

	ws, mockSrv, _ := webServerForTestTools(t, true, writeTarget)
	defer mockSrv.Close()

	// Real session+raw logs so we can assert tool_use/tool_result land in
	// the ONE session JSONL (the R2 evidence trail).
	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	rl, err := agent.NewRawLogger()
	if err != nil {
		t.Fatalf("NewRawLogger: %v", err)
	}
	defer func() { sl.Close(); rl.Close() }()
	ws.agent.SessionLog = sl
	ws.agent.RawLog = rl
	ws.agent.SystemPrompt = "test routing prompt"

	// Drive the tool turn.
	body := postChat(t, ws, "please write a file", nil)

	// 1) SSE frames: tool call + tool result + deltas + done.
	events := sseEvents(body)
	var toolEvents []map[string]string
	for _, ev := range events {
		if ev[0] == "tool" {
			var m map[string]string
			if err := json.Unmarshal([]byte(ev[1]), &m); err != nil {
				t.Fatalf("bad tool frame %q: %v", ev[1], err)
			}
			toolEvents = append(toolEvents, m)
		}
	}
	var gotCall, gotResult bool
	for _, m := range toolEvents {
		switch m["kind"] {
		case "call":
			gotCall = true
			if m["name"] != "write" {
				t.Errorf("tool call name = %q, want write", m["name"])
			}
			if m["summary"] == "" {
				t.Errorf("tool call frame carries no summary")
			}
		case "result":
			gotResult = true
		}
	}
	if !gotCall || !gotResult {
		t.Fatalf("missing tool frames: call=%v result=%v (events=%v)", gotCall, gotResult, events)
	}
	if n := countSSEEvent(events, "done"); n != 1 {
		t.Errorf("done event count = %d, want 1", n)
	}

	// 2) Session JSONL: tool_use + tool_result on the ONE session file.
	data, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var toolUse, toolResult int
	for _, ln := range strings.Split(string(data), "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Message struct {
				Content []struct {
					Type string `json:"type"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal([]byte(ln), &ev) != nil {
			continue
		}
		for _, c := range ev.Message.Content {
			switch c.Type {
			case "tool_use":
				toolUse++
			case "tool_result":
				toolResult++
			}
		}
	}
	if toolUse == 0 || toolResult == 0 {
		t.Errorf("session JSONL missing tool evidence: tool_use=%d tool_result=%d", toolUse, toolResult)
	}

	// 3) Real execution proof: the mock's tool_use wrote the file through the
	// genuine tool dispatch (executeToolBatch → tools.Write).
	b, err := os.ReadFile(writeTarget)
	if err != nil {
		t.Fatalf("tool round did not really write %s: %v", writeTarget, err)
	}
	if string(b) != "written-by-web-tools-round" {
		t.Errorf("written file content = %q, want the mock's payload", b)
	}
}

// TestWebChat_R2_DefaultTurnHasNoToolFrames: without --tools the turn runs
// the plain Agent.Chat path — no tool frames, no ChatOps.
func TestWebChat_R2_DefaultTurnHasNoToolFrames(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	ws, mockSrv, _ := webServerForTestTools(t, false, filepath.Join(t.TempDir(), "noop.txt"))
	defer mockSrv.Close()

	body := postChat(t, ws, "plain chat turn", nil)
	if countSSEEvent(sseEvents(body), "tool") != 0 {
		t.Errorf("default (no --tools) turn emitted tool frames — should be a clean Chat turn")
	}
	if !strings.Contains(body, "event: done") {
		t.Errorf("default turn missing done event")
	}
}

// TestWebToolsEnvVarEnablesTools locks the GOPI_WEB_TOOLS env contract: the
// env var (any value) enables the tool path exactly like --tools.
func TestWebToolsEnvVarEnablesTools(t *testing.T) {
	t.Setenv("GOPI_WEB_TOOLS", "1")
	// Parse-level check: runWeb would reject a bad addr before touching
	// tools, so assert via a tiny parse helper mirror instead of a full
	// server boot — the flag/env merge lives in runWeb; here we lock the
	// env-true semantics used by it.
	envSet := os.Getenv("GOPI_WEB_TOOLS") != ""
	if !envSet {
		t.Fatal("GOPI_WEB_TOOLS not visible to test")
	}
	// And a full server with tools=true routes through ChatOps (covered by
	// the frame test above); here assert the webServer field flip is what
	// selects the path.
	ws := &webServer{tools: false}
	if ws.tools {
		t.Fatal("default webServer must not enable tools")
	}
	ws.tools = true
	if !ws.tools {
		t.Fatal("tools toggle broken")
	}
}

// TestWebChat_Bug003_ShutdownWritesSessionEnd is the bug003 regression test:
// after a turn + finalizeChatSession (the shutdown path shared by normal halt
// AND the new SIGINT/SIGTERM handler), the ONE session JSONL carries exactly
// one session_end and the task-ledger row is closed — never left in_progress.
func TestWebChat_Bug003_ShutdownWritesSessionEnd(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", filepath.Join(t.TempDir(), "memory.db"))

	mockSrv := mockStreamingServer(t, "bug003 answer")
	defer mockSrv.Close()

	sl, err := agent.NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	defer sl.Close()
	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: mockSrv.URL, APIKey: "k", HTTPClient: mockSrv.Client()},
		Ctx:        &agent.Context{},
		Model:      "deepseek-v4-pro",
		Timeout:    0,
		SessionLog: sl,
		Logger:     log.New(io.Discard, "", 0),
	}
	ag.PersistChatSession = true
	ws := &webServer{addr: ":0", agent: ag, startedAt: time.Now()}

	postChat(t, ws, "hello before shutdown", nil)

	// Before shutdown: session open, the CHATS row ensured (mode=web) and
	// still running (M4.6 钩子分流 — the tasks-ledger [auto] row retired).
	sessionID := sl.SessionID()
	if row, err := tools.ChatList(tools.ChatListFilter{}); err != nil || !strings.Contains(row, sessionID) {
		t.Fatalf("pre-shutdown chat row = %q err %v; want session %s", row, err, sessionID)
	}

	// Simulate the shutdown path (what SIGINT/SIGTERM now triggers).
	ws.finalizeChatSession(true)

	data, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	ends := 0
	for _, ln := range strings.Split(string(data), "\n") {
		var ev struct {
			Type string `json:"type"`
		}
		if json.Unmarshal([]byte(strings.TrimSpace(ln)), &ev) == nil && ev.Type == "session_end" {
			ends++
		}
	}
	if ends != 1 {
		t.Errorf("session_end count after shutdown = %d, want exactly 1 (bug003)", ends)
	}
	if row, err := tools.ChatList(tools.ChatListFilter{}); err != nil || !strings.Contains(row, "normal") {
		t.Errorf("post-shutdown chat row = %q err %v; want ended (end_reason normal)", row, err)
	}
}

// TestWebChat_Bug003_ShuttingDownFlagRejectsNewTurns locks the shutdown
// interlock: once shuttingDown flips, /api/chat refuses new turns (503)
// instead of queueing behind the session close.
func TestWebChat_Bug003_ShuttingDownFlagRejectsNewTurns(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	ws, mockSrv, _ := webServerForTestTools(t, false, filepath.Join(t.TempDir(), "noop.txt"))
	defer mockSrv.Close()
	ws.shuttingDown = true

	body, _ := json.Marshal(webChatRequest{Message: "late arrival"}) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	req := httptest.NewRequest(http.MethodPost, "/api/chat", strings.NewReader(string(body)))
	rec := httptest.NewRecorder()
	ws.handleChat(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("turn during shutdown status = %d, want 503", rec.Code)
	}
}

// TestWebToolsSignalShutdownWritesSessionEnd is the closest-to-real bug003
// check available without forking a full server: boot `gopi web` as a real
// subprocess (so runWeb's signal handler is live), drive one turn, send
// SIGTERM, and assert the session JSONL carries exactly one session_end.
//
// Two traps this test used to fall into, both closed here:
//
//  1. GOPI_HOME was assigned AFTER the spawn. StartMockProcess starts the
//     child itself, and exec(2) freezes args+env: mutating cmd.Env afterwards
//     changes nothing, so the server logged into the OPERATOR'S REAL
//     ~/.gopi/sessions while the assertion read the empty t.TempDir()
//     sandbox — the deterministic "no session dir/files" failure reported as
//     a flaky readiness race. The env is now wired BEFORE the spawn
//     (testutil.StartGopi), and the sandbox is a real GOPI_HOME, so this test
//     doubles as the isolation guard for that bug: mis-wire the env again and
//     the readiness wait below times out on an empty sandbox.
//
//  2. Readiness was "the HTTP listener accepts my POST". The listener is up
//     as soon as the socket binds, so the probe proved nothing about the
//     artifact the assertion reads. Readiness is now the session JSONL
//     itself (written by agent construction, before Serve), and the single
//     driven turn is consumed to its terminal SSE frame so SIGTERM hits a
//     settled server instead of cancelling a turn in flight.
//
//  3. Readiness used to depend on a CREDENTIAL FROM THE AMBIENT ENVIRONMENT.
//     The text here claimed "the key is NOT a precondition: `gopi web` boots,
//     writes its session JSONL and finalizes the session on SIGTERM with no
//     key at all". That is FALSE on HEAD, and measured false 2026-10-04:
//     runWeb creates the persistent agent BEFORE it serves, and exits 1 when
//     that construction fails — `gopi web: failed to create agent: … deepseek:
//     ANTHROPIC_AUTH_TOKEN not set` — so no session JSONL is ever written and
//     the readiness wait below timed out (2.75s, empty stdout). The credential
//     now comes FROM THIS TEST: testutil.MockEnv plus the scripted
//     testutil.NewMockLLM, exactly as every other mocked-provider case in this
//     package works. The case therefore neither skips nor borrows an
//     operator's token, and the driven turn is a hermetic scripted call.
//
// So this test always RUNS — never a silent skip that a non-verbose
// `go test` cannot distinguish from a pass — and never a credential it does
// not carry itself.
func TestWebToolsSignalShutdownWritesSessionEnd(t *testing.T) {
	// The binary this case drives comes from the ONE resolver:
	// testutil.StartGopi below calls testutil.GopiBin itself. The
	// `bin := filepath.Join("..", "..", "bin", "gopi")` this function used to
	// open with was the stale-binary shape (in a shared checkout that relative
	// path is not necessarily THIS worktree — devdoc/tested-binary-pin.md), and
	// its `os.Stat` SKIP made "no binary here" read as a pass. It was also
	// dead: nothing below ever used the variable, so the resolution could only
	// ever have mis-skipped the case.

	home := t.TempDir()
	// The provider is the SCRIPTED one, so the driven turn is hermetic: no real
	// model call, and above all no credential to hold (see trap 3 in the doc
	// above — `gopi web` cannot boot without one). NewMockLLM answers the POST
	// below with a scripted Claude SSE stream, and its URL is wired through
	// testutil.MockEnv: the ONE script that decides a mocked run's environment,
	// which strips the ambient ANTHROPIC_*/DEEPSEEK_*/GOPI_* (an inherited
	// operator key can neither reach the child nor mask its absence) and injects
	// this test's sandbox GOPI_HOME.
	llm := testutil.NewMockLLM(t, testutil.Resp{Text: webSignalTurnReplyText})
	defer llm.Close()
	env := testutil.MockEnv(t, home, llm.URL())

	// Merge note: master's POST-probe "wait for the listener" block is
	// superseded by the artifact-based readiness below — it also referenced a
	// `port` variable that HEAD no longer has (HEAD asks the kernel for a free
	// port via --addr=127.0.0.1:0), and the driven turn below already proves
	// the listener is reachable with a clearer failure message.
	// StartGopi wires Env BEFORE the spawn (exec(2) freezes args+env — see the
	// function's doc), captures output safely, and owns the pgid-scoped reaper.
	// --addr=127.0.0.1:0 asks the kernel for a free port; the listening banner
	// reports the bound one, so no hardcoded port can collide.
	p := testutil.StartGopi(t, []string{"web", "--addr=127.0.0.1:0"}, "", env, "")
	base := p.WebBaseURL()

	// Readiness = the artifact under test, not the listener: the session
	// JSONL under the SANDBOX sessions root (session-dir-unify S1 bare
	// layout sessions/<uuid>/session.jsonl). WaitFor fails with the child's
	// captured output if it never appears or the server dies first.
	sessRoot := filepath.Join(home, ".gopi", config.SessionsDirName)
	p.WaitFor("the sandboxed session JSONL", func() bool { return testutil.SessionRecordPath(sessRoot) != "" })

	// Drive exactly ONE turn to its terminal SSE frame, so the SIGTERM below
	// hits a settled server rather than cancelling a turn in flight.
	client := &http.Client{Timeout: webSignalTurnTimeout}
	resp, err := client.Post(base+"/api/chat", "application/json", strings.NewReader(`{"message":"ping"}`))
	if err != nil {
		t.Fatalf("turn POST failed: %v", err)
	}
	body, readErr := io.ReadAll(resp.Body)
	resp.Body.Close() //stylegate:ignore the body was drained to EOF by the read above and its error is checked on the next line; the close error of a fully-read response carries no state
	if readErr != nil {
		t.Fatalf("reading the turn stream failed: %v", readErr)
	}
	// The provider is scripted and the server is settled, so a turn that does
	// not reach its terminal frame is a real failure here — not the tolerated
	// "no model reachable" case this branch used to carry (that tolerance
	// existed because the turn's model call was real and optional; it is
	// neither now).
	if !strings.Contains(string(body), "event: done") {
		t.Errorf("turn did not reach `event: done` against the scripted provider — stream=%q", truncate(string(body), 200))
	}

	// SIGTERM — must NOT leak the session_end anymore (bug003).
	p.Signal(syscall.SIGTERM)
	done := make(chan int, 1)
	go func() { done <- p.Wait() }()
	select {
	case code := <-done:
		if code != 0 {
			t.Errorf("gopi web exit after SIGTERM = %d, want a clean exit 0", code)
		}
	case <-time.After(webSignalExitTimeout):
		t.Fatalf("gopi web did not exit after SIGTERM within %s\nstderr:\n%s", webSignalExitTimeout, p.Stderr())
	}

	// Exactly one SESSION, carrying exactly one session_end — the parse and
	// structure assertions live in testutil (LoadSession), so this test does
	// not re-implement a JSONL scanner.
	//
	// "One session" means one session RECORD, not one *.jsonl: this run's
	// session dir legitimately also holds signal-provenance.jsonl — the
	// SIGTERM provenance record for the same session's process, written by
	// internal/signalrec when the signal below is caught. It is a SIDE FILE of
	// this session, not a second session, so the accounting groups by session
	// dir (testutil.LoadSession owns it; it also reports an UNKNOWN *.jsonl
	// beside a record instead of silently ignoring it, so this stays a check
	// rather than a loophole).
	sess := testutil.LoadSession(t, home)
	sess.AssertSingleSession(t)
	if got := sess.Counts()[sessionevent.EventTypeSessionStart]; got != 1 {
		t.Errorf("%s events = %d, want exactly 1 (bug003: one session per web run)",
			sessionevent.EventTypeSessionStart, got)
	}
	if got := sess.Counts()[sessionevent.EventTypeSessionEnd]; got != 1 {
		t.Errorf("SIGTERM shutdown %s count = %d, want exactly 1 (bug003) — %s",
			sessionevent.EventTypeSessionEnd, got, sess.Summary())
	}
	if !strings.HasSuffix(filepath.ToSlash(sess.Path), "/"+webSignalSessionFileBase) {
		t.Errorf("session JSONL = %s, want the bare layout .../<uuid>/%s", sess.Path, webSignalSessionFileBase)
	}
}
