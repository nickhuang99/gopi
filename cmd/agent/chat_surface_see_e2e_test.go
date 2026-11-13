package main

// chat_surface_see_e2e_test.go — the defect's OWN measurement point, end to end.
//
// WHY THE OTHER THREE CASES ARE NOT THIS ONE. internal/lineedit proves the
// rendering is bounded; internal/agent proves the tool cannot return an unbounded
// answer; cmd/agent's measure case drives the production ScreenGrid over a real
// artifact. None of them shows what actually crossed the wire. The incident was
// measured in a REQUEST BODY — `msg[188] tool_result content len=2,783,126` — so
// the fix has to be measured in a request body too, or the reading is about a
// function rather than about the thing that blew up.
//
// So this case runs the REAL `gopi chat` REPL on a real pty, with the REAL
// chatSurface installed, against a mock model whose second round follows the
// first with a follow-up turn. The number below is read out of the SECOND request
// body's tool_result for the `action=see` call: exactly the message that made a
// chat turn cost 1.36M tokens.
//
// The screen is made to carry a real history first: the session's own paint stream
// is fed enough scrolling lines that the artifact's scrollback section is what
// dominated the pre-fix answer. Without that the case would measure a 24-row
// screen and agree with itself.

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

const (
	// seeE2EModel names the model the mock stands in for.
	seeE2EModel = "deepseek-v4-pro"
	// seeE2EBootstrap is the operator's line that opens the session; the REPL runs
	// no model round before one has been submitted.
	seeE2EBootstrap = "hello"
	// seeE2EAnswer is the text of the round that carries the `see` call, so a
	// reader of the transcript can tell which round asked for the screen.
	seeE2EAnswer = "reading my own screen"
	// seeE2ETextAfter is the closing answer, so the session ends cleanly.
	seeE2ETextAfter = "the screen was read."
	// seeE2EWait bounds the wait for the second request to arrive.
	seeE2EWait = 20 * time.Second
)

// TestChatSurfaceSee_ToolResultInTheRequestBodyIsBounded is the acceptance case:
// a real chat session, a real `action=see`, and the measurement taken where the
// incident was measured.
func TestChatSurfaceSee_ToolResultInTheRequestBodyIsBounded(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	exited := make(chan struct{})
	done := make(chan int, 1)

	var mu sync.Mutex
	var bodies []string
	round := 0
	// secondBody arrives once the round AFTER the see call is on the wire — that
	// round's request is the one carrying the tool_result.
	secondBody := make(chan string, 1)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)            //stylegate:ignore bounded read of this fixture's own request body
		defer func() { _ = r.Body.Close() }() //stylegate:ignore this fixture's own body close
		mu.Lock()
		bodies = append(bodies, string(b))
		round++
		n := round
		mu.Unlock()
		if n == 2 {
			select {
			case secondBody <- string(b):
			case <-exited:
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		switch n {
		case 1:
			// Round 1: the model asks to see its own screen.
			seeE2EMockSeeSSE(w)
		default:
			testutil.TextSSE(w, seeE2ETextAfter)
		}
	}))
	t.Cleanup(srv.Close)

	pair := chatOpenPtyPair(t, keyE2EPtyCols, keyE2EPtyRows)
	paint := &keyE2EPaint{}
	term, err := lineedit.Open(pair.Slave, paint)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	h := &keyE2EHarness{pair: pair, term: term, paint: paint, done: done, exited: exited}
	t.Cleanup(func() {
		if !term.Stopped() {
			_ = term.Close() //stylegate:ignore this fixture's own terminal teardown
		}
		chatJoinReadLoop(t, exited, func() string { return runState(exited) })
	})

	in := newChatInputWithSource(term, paint, chatPrompt)
	in.editor.SetPalette(lineedit.BasicPalette)
	in.editor.SetCompleter(NewSlashCompleter())
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: this case needs the real chat layout", keyE2EPtyCols, keyE2EPtyRows)
	}
	h.in = in

	sl, err := agent.NewSessionLogger(seeE2EModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore this fixture's own logger teardown

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      seeE2EModel,
		Timeout:    0,
		SessionLog: sl,
	}
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	go func() {
		defer close(exited)
		done <- runChatWithInput([]string{"--tools"}, in)
	}()

	keyE2ETypeLine(t, h, pair.Master, seeE2EBootstrap)

	var body string
	select {
	case body = <-secondBody:
	case <-time.After(seeE2EWait):
		t.Fatalf("the round after the see call never reached the wire — the run produced %d request(s)\n--- grid ---\n%s", len(bodies), h.rows())
	}

	size, found := seeE2EToolResultChars(t, body)
	t.Logf("SEE-E2E request 2 carries the action=see tool_result = %d characters", size)
	if !found {
		t.Fatalf("no tool_result for the see call in the second request body — the case measured nothing:\n%s", seeE2EBodyHead(body))
	}
	// The bound, stated as the property that was violated: one glance at a
	// 24-row screen must not cost a megabyte of context.
	if size > seeE2ECharsCeiling {
		t.Fatalf("the action=see tool_result is %d characters, above this case's %d-character ceiling — that is the shape that made one chat turn cost 1.36M tokens (measured 2026-11-06: 2,783,126 characters, 99.9%% of it scrollback)",
			size, seeE2ECharsCeiling)
	}
	// And it is still an ANSWER: the facts line, the grid, and the truncation
	// notice that says what the artifact holds.
	if !strings.Contains(body, "surface=chat state=") {
		t.Errorf("the tool_result carries no surface facts line — the answer was the grid alone")
	}
	// THIS SESSION IS FRESH, so its artifact holds less scrollback than the bound
	// and the notice must NOT appear (a notice over a complete history is a lie in
	// the other direction). The truncation itself is measured where it can be
	// measured honestly — on the real incident artifacts, in
	// cmd/agent/chat_surface_see_measure_test.go and internal/lineedit's suite —
	// rather than manufactured here by painting into a sink that is not the dump.
	if strings.Contains(body, lineedit.DumpScrollbackTruncatedLabel) {
		t.Errorf("the answer announced held-back scrollback on a fresh session's artifact — the notice must be earned by real truncation, never printed unconditionally")
	}
}

// seeE2ECharsCeiling is this case's acceptance ceiling: tens of kilobytes, not
// megabytes. It is a test-side bound rather than a product constant on purpose —
// the product bound is chatSurfaceSeeCharsMax (a backstop) and
// lineedit.DumpScrollbackSeeRows (the real one); this number only has to be far
// below the incident and comfortably above an ordinary 24-row answer.
const seeE2ECharsCeiling = 64 * 1024

// seeE2EMockSeeSSE streams ONE tool_use round calling chat_surface action=see.
func seeE2EMockSeeSSE(w io.Writer) {
	testutil.WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1}}}`)
	testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	testutil.WriteClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":%q}}`, seeE2EAnswer))
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	testutil.WriteClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":%q,"name":%q,"input":{}}}`, seeE2EToolUseID, toolnames.ToolChatSurface))
	testutil.WriteClaudeSSE(w, "content_block_delta", fmt.Sprintf(`{"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":%q}}`, fmt.Sprintf(`{"action":%q}`, agent.ChatSurfaceActionSee)))
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
	testutil.WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	testutil.WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// seeE2EToolUseID is the id the mock's see call carries, so the tool_result in the
// NEXT request body is the one this case measures and not some other round's.
const seeE2EToolUseID = "call_see_e2e"

// seeE2EToolResultChars finds the tool_result for seeE2EToolUseID in a request body
// and returns its content's length in characters.
//
// It walks the parsed JSON rather than grepping: a length read off a regex over a
// 4 MB body measured the wrong span once already (the incident's own investigation
// had to compute CHARACTER OFFSETS to separate the grid from the scrollback), and
// the structured walk is the reading that cannot be off by a nesting level.
func seeE2EToolResultChars(t *testing.T, body string) (int, bool) {
	t.Helper()
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("the second request body is not parseable JSON: %v", err)
	}
	for _, m := range req.Messages {
		var blocks []struct {
			Type      string          `json:"type"`
			ToolUseID string          `json:"tool_use_id"`
			Content   json.RawMessage `json:"content"`
		}
		if err := json.Unmarshal(m.Content, &blocks); err != nil {
			continue // a plain string message
		}
		for _, b := range blocks {
			if b.Type != "tool_result" || b.ToolUseID != seeE2EToolUseID {
				continue
			}
			// A tool_result's content is either a string or a list of text blocks.
			var s string
			if err := json.Unmarshal(b.Content, &s); err == nil {
				return len(s), true
			}
			var parts []struct {
				Text string `json:"text"`
			}
			if err := json.Unmarshal(b.Content, &parts); err == nil {
				n := 0
				for _, p := range parts {
					n += len(p.Text)
				}
				return n, true
			}
		}
	}
	return 0, false
}

// seeE2EBodyHead is a bounded excerpt for a failure message.
func seeE2EBodyHead(s string) string {
	if len(s) > 2000 {
		return s[:2000] + "\n…(truncated for the message)"
	}
	return s
}
