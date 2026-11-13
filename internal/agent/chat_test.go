package agent

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// TestAgentChat_SingleRoundNoTools verifies the simplified chat path
// (Agent.Chat) is genuinely lightweight: one API round-trip (not the ReAct
// loop's confirmation rounds), no tools sent on the wire, a single returned
// answer, and the answer fed back into context so the next turn can seed on
// it. This is the "differentiate chat mode from the full task loop" guarantee
// the Epic asks for — chat is Q&A, not a task run.
func TestAgentChat_SingleRoundNoTools(t *testing.T) {
	var callCount int32
	gotText := ""
	hadTools := false
	hadAnyToolBlock := false

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&callCount, 1)
		// Capture the request body to assert no tools are advertised.
		body, err := io.ReadAll(r.Body)
		if err == nil {
			var req deepseek.ClaudeRequest
			if json.Unmarshal(body, &req) == nil {
				hadTools = len(req.Tools) > 0
				for _, m := range req.Messages {
					for _, b := range m.Content {
						if b.Type == "tool_use" || b.Type == "tool_result" {
							hadAnyToolBlock = true
						}
					}
				}
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"Hello, ", "chat ", "mode!"}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	ag.OnText = func(delta string) { gotText += delta }

	result, err := ag.Chat("say hi", nil)
	if err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	want := "Hello, chat mode!"
	if result != want {
		t.Errorf("result = %q, want %q", result, want)
	}
	if gotText != want {
		t.Errorf("OnText accumulated = %q, want %q", gotText, want)
	}
	if n := atomic.LoadInt32(&callCount); n != 1 {
		t.Errorf("Chat() made %d API calls, want exactly 1 (no ReAct confirmation rounds)", n)
	}
	if hadTools {
		t.Error("Chat() request advertised tools — chat mode must send no tool defs")
	}
	if hadAnyToolBlock {
		t.Error("Chat() request carried tool_use/tool_result content blocks — chat mode is Q&A only")
	}

	// The answer must be recorded in Ctx.Messages so the next turn can seed on
	// it (multi-turn continuity).
	if len(ag.Ctx.Messages) != 2 {
		t.Fatalf("Ctx.Messages has %d messages, want 2 (user + assistant)", len(ag.Ctx.Messages))
	}
	last := ag.Ctx.Messages[len(ag.Ctx.Messages)-1]
	if last.Role != "assistant" || last.Content != want {
		t.Errorf("last Ctx message = %+v, want assistant %q", last, want)
	}
}

// TestAgentChat_SeedsHistory verifies Chat seeds the provided history before
// the user's turn and appends the user message verbatim (no EPIC framing) —
// so conversation context genuinely accumulates across turns.
func TestAgentChat_SeedsHistory(t *testing.T) {
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		claudeTextStreamFragmented(w, []string{"ok"}, "end_turn")
	}))
	defer server.Close()

	ag := newTestAgent(t, server)
	history := []deepseek.Message{
		{Role: "user", Content: "remember ZEBRA"},
		{Role: "assistant", Content: "ZEBRA noted."},
	}
	if _, err := ag.Chat("what was the codeword?", history); err != nil {
		t.Fatalf("Chat() error: %v", err)
	}

	// Body must contain both the seeded history messages and the new turn,
	// and must NOT contain [EPIC GOAL] framing.
	for _, want := range []string{"remember ZEBRA", "ZEBRA noted.", "what was the codeword?"} {
		if !strings.Contains(gotBody, want) {
			t.Errorf("Chat() body missing history/user text %q", want)
		}
	}
	if strings.Contains(gotBody, "[EPIC GOAL]") {
		t.Error("Chat() body contains [EPIC GOAL] framing — chat mode must not frame turns as Epic tasks")
	}
}
