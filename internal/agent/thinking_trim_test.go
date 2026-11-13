package agent

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// buildTrimCtx returns a Context with the canonical P2 test shape:
//
//	user → assistant (text + thinking) → tool → assistant (tool_use + thinking)
//
// Role alternation and every non-reasoning field must survive the strip.
func buildTrimCtx() *Context {
	c := &Context{
		SystemPrompt:   "kernel prompt",
		ProjectContext: "project context",
	}
	c.AddUserMessage("hello")
	c.AddAssistantMessageWithThinking("first reply", nil,
		"thinking alpha", "sig-alpha")
	c.AddToolResult("tool_1", "tool result payload")
	c.AddMessage(deepseek.Message{
		Role:              "assistant",
		Content:           "calling a tool",
		ToolCalls:         []deepseek.ToolCall{{ID: "tool_1", Name: "bash", RawArgs: `{"command":"ls"}`}},
		ReasoningContent:  "thinking beta",
		ThinkingSignature: "sig-beta",
	})
	return c
}

// TestWireMessagesStripsByDefault — the P2 core: default (keepThinking=false)
// strips assistant reasoning from the WIRE while preserving role alternation,
// text content, tool_use blocks and tool results. The in-memory history keeps
// its reasoning (wire-only strip — session JSONL and compaction still see it).
func TestWireMessagesStripsByDefault(t *testing.T) {
	c := buildTrimCtx()
	msgs := c.WireMessages(false)

	if len(msgs) != 6 { // 2 system + 4 conversation
		t.Fatalf("expected 6 wire messages, got %d", len(msgs))
	}

	// Role alternation must be preserved exactly.
	wantRoles := []string{"system", "system", "user", "assistant", "tool", "assistant"}
	for i, want := range wantRoles {
		if msgs[i].Role != want {
			t.Fatalf("msg[%d]: role alternation broken — want %q, got %q", i, want, msgs[i].Role)
		}
	}

	// Reasoning stripped from every assistant message.
	for i, m := range msgs {
		if m.Role == "assistant" && m.ReasoningContent != "" {
			t.Fatalf("msg[%d]: reasoning not stripped (got %q)", i, m.ReasoningContent)
		}
		if m.Role == "assistant" && m.ThinkingSignature != "" {
			t.Fatalf("msg[%d]: thinking signature not stripped (got %q)", i, m.ThinkingSignature)
		}
	}

	// Text content intact.
	if msgs[3].Content != "first reply" {
		t.Fatalf("assistant text damaged: %q", msgs[3].Content)
	}
	// Tool result intact.
	if msgs[4].Content != "tool result payload" || msgs[4].ToolCallID != "tool_1" {
		t.Fatalf("tool result damaged: %+v", msgs[4])
	}
	// Tool_use block intact (ID/name/args survive the strip).
	tc := msgs[5].ToolCalls
	if len(tc) != 1 || tc[0].ID != "tool_1" || tc[0].Name != "bash" {
		t.Fatalf("tool_use block damaged: %+v", tc)
	}

	// Wire-only: in-memory history keeps its reasoning.
	if c.Messages[1].ReasoningContent != "thinking alpha" {
		t.Fatalf("in-memory reasoning was mutated by the wire strip")
	}
	if c.Messages[3].ReasoningContent != "thinking beta" {
		t.Fatalf("in-memory reasoning was mutated by the wire strip")
	}
}

// TestWireMessagesKeepThinking — the A/B control arm (GOPI_KEEP_THINKING=1):
// reasoning must be replayed verbatim, untouched.
func TestWireMessagesKeepThinking(t *testing.T) {
	c := buildTrimCtx()
	msgs := c.WireMessages(true)

	got := map[int]string{}
	for i, m := range msgs {
		if m.Role == "assistant" {
			got[i] = m.ReasoningContent
		}
	}
	if got[3] != "thinking alpha" || got[5] != "thinking beta" {
		t.Fatalf("keep-thinking did not preserve reasoning: %+v", got)
	}
	if msgs[5].ThinkingSignature != "sig-beta" {
		t.Fatalf("keep-thinking did not preserve signature: %q", msgs[5].ThinkingSignature)
	}
}

// TestNewAgentKeepThinkingEnv — the env switch (Rule 2: exactly one read in
// NewAgent): unset → strip (default), GOPI_KEEP_THINKING=1 → keep.
func TestNewAgentKeepThinkingEnv(t *testing.T) {
	newTestAgentEnv(t)
	t.Setenv("GOPI_KEEP_THINKING", "")
	ag, err := NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if ag.KeepThinking {
		t.Fatalf("default must strip thinking (KeepThinking=false), got true")
	}

	t.Setenv("GOPI_KEEP_THINKING", "1")
	ag, err = NewAgent("test-model")
	if err != nil {
		t.Fatalf("NewAgent: %v", err)
	}
	if !ag.KeepThinking {
		t.Fatalf("GOPI_KEEP_THINKING=1 must set KeepThinking=true")
	}
}

// TestWireMessagesEmptyContext — degenerate case: no messages at all still
// returns a valid empty wire slice.
func TestWireMessagesEmptyContext(t *testing.T) {
	c := &Context{}
	msgs := c.WireMessages(false)
	if len(msgs) != 0 {
		t.Fatalf("expected empty wire, got %d messages", len(msgs))
	}
}

// TestAgent_Run_ToolResultPayloadOnce is the regression test for the
// payload-duplication bug found during the P2 A/B exercise: the tool-result
// assembly used to append the raw payload twice
// ("<payload>[tool_use id: X]\n<payload>"), doubling every tool result on the
// wire and in the session log. The tick must wrap the STAMPED result so the
// payload appears exactly once.
func TestAgent_Run_ToolResultPayloadOnce(t *testing.T) {
	type captured struct{ body string }
	var rounds []captured

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "text/event-stream")
		rounds = append(rounds, captured{body: readBody(t, r)})

		switch callCount {
		case 1:
			// Round 1: a bash call that FAILS — its output becomes a tool_result.
			claudeToolUseStreamID(w, "call_p1", "bash",
				map[string]interface{}{"command": "ls /nonexistent"})
		default:
			// Round 2: all-completed end_turn → run exits.
			claudeTextStream(w, completedTaskListResponse, "end_turn")
		}
	}))
	defer server.Close()

	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    server.URL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 3,
	}

	if _, err := ag.Run("Do the payload-once task"); err != nil {
		t.Fatalf("Run() unexpected error: %v", err)
	}
	if len(rounds) < 2 {
		t.Fatalf("expected at least 2 rounds, got %d", len(rounds))
	}

	body := rounds[1].body
	// Stamp must still be present (verbatim-copy enabler for memory_feedback
	// / context_trim).
	if !strings.Contains(body, "[tool_use id: call_p1]") {
		t.Fatalf("round 2 request must carry the stamped tool_use id")
	}
	// The failure output must appear EXACTLY ONCE — the duplication bug made
	// it appear twice (tick-decorated copy + verbatim copy).
	want := "cannot access '/nonexistent'"
	if got := strings.Count(body, want); got != 1 {
		t.Fatalf("tool-result payload must appear exactly once on the wire, got %d (duplication bug regression)", got)
	}
}
