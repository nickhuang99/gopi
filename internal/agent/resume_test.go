package agent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
)

// rawLogForTest writes a minimal raw log containing one → REQUEST checkpoint
// (single-line JSON payload, as the raw logger writes it) and returns the path.
func rawLogForTest(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "gopi-raw-20260826-120000.log")
	if err := os.WriteFile(path, []byte("→ REQUEST\n"+body+"\n"), 0644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}
	return path
}

// TestLoadCheckpoint_ExtractsLastRequest builds a raw log with two REQUEST
// checkpoints and verifies LoadCheckpoint returns the LAST one, reconstructing
// user/assistant(tool_use)/tool history faithfully.
func TestLoadCheckpoint_ExtractsLastRequest(t *testing.T) {
	// Request #1 — an earlier checkpoint (must NOT win).
	earlyReq := deepseek.ClaudeRequest{
		Model: "deepseek-v4-flash", MaxTokens: 16384,
		Messages: []deepseek.ClaudeMessage{
			{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "early instruction"}}},
		},
	}
	earlyBytes, _ := json.Marshal(earlyReq) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)

	// Request #2 — the final checkpoint: user epic, assistant tool_use,
	// tool_result, assistant text.
	lastReq := deepseek.ClaudeRequest{
		Model: "deepseek-v4-flash", MaxTokens: 16384,
		System: []deepseek.ClaudeSystemBlock{{Type: "text", Text: "kernel"}, {Type: "text", Text: "GOPI.md"}},
		Messages: []deepseek.ClaudeMessage{
			{Role: "user", Content: []deepseek.ClaudeContentBlock{{Type: "text", Text: "[EPIC GOAL]\nbuild X"}}},
			{Role: "assistant", Content: []deepseek.ClaudeContentBlock{
				{Type: "thinking", Thinking: "need to inspect", Signature: "sig-123"},
				{Type: "text", Text: "Let me look."},
				{Type: "tool_use", ID: "call_abc", Name: "bash", Input: map[string]interface{}{"command": "ls"}},
			}},
			{Role: "user", Content: []deepseek.ClaudeContentBlock{
				{Type: "tool_result", ToolUseID: "call_abc", Content: "exit=0\nfile.txt"},
			}},
			{Role: "assistant", Content: []deepseek.ClaudeContentBlock{
				{Type: "text", Text: "Found it."},
			}},
		},
	}
	lastBytes, _ := json.Marshal(lastReq) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)

	path := filepath.Join(t.TempDir(), "gopi-raw-test.log")
	content := "← CHUNK\n" +
		"event: ping\ndata: {}\n" +
		"→ REQUEST\n" + string(earlyBytes) + "\n" +
		"← CHUNK\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"hi\"}}\n" +
		"→ REQUEST\n" + string(lastBytes) + "\n" +
		"← RESPONSE\n{}\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write raw log: %v", err)
	}

	req, history, systemTexts, err := LoadCheckpoint(path)
	if err != nil {
		t.Fatalf("LoadCheckpoint: %v", err)
	}
	if req.Model != "deepseek-v4-flash" {
		t.Errorf("model = %q, want deepseek-v4-flash", req.Model)
	}
	if len(systemTexts) != 2 || systemTexts[0] != "kernel" || systemTexts[1] != "GOPI.md" {
		t.Errorf("systemTexts = %v, want [kernel GOPI.md]", systemTexts)
	}

	// 4 messages: user, assistant (thinking+text+tool_use), tool, assistant.
	if len(history) != 4 {
		t.Fatalf("history len = %d, want 4: %+v", len(history), history)
	}
	if history[0].Role != "user" || !strings.Contains(history[0].Content, "EPIC GOAL") {
		t.Errorf("history[0] = %+v, want epic user message", history[0])
	}
	if history[1].Role != "assistant" {
		t.Fatalf("history[1].Role = %q, want assistant", history[1].Role)
	}
	if history[1].ReasoningContent != "need to inspect" || history[1].ThinkingSignature != "sig-123" {
		t.Errorf("history[1] thinking = %q/%q, want need to inspect/sig-123",
			history[1].ReasoningContent, history[1].ThinkingSignature)
	}
	if len(history[1].ToolCalls) != 1 || history[1].ToolCalls[0].ID != "call_abc" || history[1].ToolCalls[0].Name != "bash" {
		t.Errorf("history[1].ToolCalls = %+v, want call_abc/bash", history[1].ToolCalls)
	}
	if history[2].Role != "tool" || history[2].ToolCallID != "call_abc" || !strings.Contains(history[2].Content, "file.txt") {
		t.Errorf("history[2] = %+v, want tool result for call_abc", history[2])
	}
	if history[3].Role != "assistant" || history[3].Content != "Found it." {
		t.Errorf("history[3] = %+v, want assistant text", history[3])
	}
}

// TestLoadCheckpoint_NoRequest reports the error path for a log without any
// → REQUEST marker.
func TestLoadCheckpoint_NoRequest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(path, []byte("← CHUNK\ndata: {}\n"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, _, _, err := LoadCheckpoint(path); err == nil || !strings.Contains(err.Error(), "no → REQUEST") {
		t.Fatalf("want no-REQUEST error, got %v", err)
	}
}

// TestLoadCheckpoint_MissingFile checks the open-error path.
func TestLoadCheckpoint_MissingFile(t *testing.T) {
	if _, _, _, err := LoadCheckpoint("/nonexistent/gopi-raw-x.log"); err == nil {
		t.Fatal("want error for missing file, got nil")
	}
}

// TestResume_ContinuesFromHistory is the core resume integration test: a
// fresh agent's Ctx is seeded with the recovered history and the continuation
// message is appended; the mock server verifies the FIRST resumed request
// carries the recovered messages verbatim (no [EPIC GOAL] re-framing, no
// task-start search injection) followed by the continuation instruction.
func TestResume_ContinuesFromHistory(t *testing.T) {
	history := []deepseek.Message{
		{Role: "user", Content: "[EPIC GOAL]\nwrite a poem\n\nBased on this Epic, generate an initial task list."},
		{Role: "assistant", Content: "I will write a poem.", ToolCalls: []deepseek.ToolCall{
			{ID: "call_1", Name: "write", Arguments: map[string]interface{}{"path": "/tmp/p.txt", "content": "roses"}},
		}},
		{Role: "tool", Content: "exit=0\nwritten", ToolCallID: "call_1"},
		{Role: "assistant", Content: "The poem file was written.", ThinkingSignature: "sig-x", ReasoningContent: "done thinking"},
	}

	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")

		if callCount == 1 {
			// The resumed first request MUST carry the recovered history
			// verbatim — first user message is the epic (not re-framed), the
			// tool_use/tool_result pair intact, thinking+sig preserved.
			if len(req.Messages) < 5 {
				t.Fatalf("resumed request has %d messages, want >= 5 (history+continuation)", len(req.Messages))
			}
			first := req.Messages[0]
			if first.Role != "user" || !strings.Contains(first.Content[0].Text, "[EPIC GOAL]") {
				t.Errorf("first message = %+v, want original epic user message (no re-framing)", first)
			}
			foundToolUse, foundToolResult := false, false
			var lastAsst *deepseek.ClaudeMessage
			for i := range req.Messages {
				m := &req.Messages[i]
				if m.Role == "assistant" {
					lastAsst = m
				}
				for _, b := range m.Content {
					if b.Type == "tool_use" && b.ID == "call_1" {
						foundToolUse = true
					}
					if b.Type == "tool_result" && b.ToolUseID == "call_1" {
						foundToolResult = true
					}
				}
			}
			if !foundToolUse || !foundToolResult {
				t.Errorf("resumed request lost tool_use/tool_result pair (use=%v result=%v)", foundToolUse, foundToolResult)
			}
			if lastAsst == nil {
				t.Fatal("no assistant message in resumed request")
			}
			// P2 thinking-trim default: assistant reasoning was STRIPPED on
			// the interrupted session's own wire too (WireMessages with
			// KeepThinking=false), so the resumed request replays the same
			// wire shape — no thinking blocks, exactly as the API accepted
			// during the interrupted run. The in-memory history keeps
			// ReasoningContent (for compaction/logging), only the wire drops
			// it. Assert the wire has NO thinking block (consistent with the
			// interrupted wire, so the API cannot newly reject it).
			for _, b := range lastAsst.Content {
				if b.Type == "thinking" {
					t.Errorf("resumed wire carries a thinking block (P2 default strips it, same as interrupted wire): %+v", lastAsst.Content)
				}
			}
			// Last message must be the continuation instruction.
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "user" || !strings.Contains(last.Content[0].Text, "Continue from where you left off") {
				t.Errorf("last message = %+v, want continuation instruction", last)
			}
			// No [EPIC GOAL] re-framing of the continuation.
			if strings.Contains(last.Content[0].Text, "Based on this Epic") {
				t.Errorf("continuation was re-framed as an Epic goal: %q", last.Content[0].Text)
			}
			claudeTextStream(w, "Continuing. I will now finish the task.", "end_turn")
			return
		}
		// Second round: confirmation end_turn → exit (R3 needs 2 consecutive,
		// or all-completed task list + substantive answer — plain end_turn
		// needs the confirmation round).
		claudeTextStream(w, "All done.", "end_turn")
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
		MaxIterations: 5,
	}

	result, err := ag.Resume("", history)
	if err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if !strings.Contains(result, "Continuing") {
		t.Errorf("result = %q, want resumed answer", result)
	}
	if callCount < 2 {
		t.Errorf("expected >= 2 API calls, got %d", callCount)
	}
}

// TestResume_ExplicitContinuation verifies an explicit user continuation is
// appended verbatim instead of the default continuation message.
func TestResume_ExplicitContinuation(t *testing.T) {
	history := []deepseek.Message{
		{Role: "user", Content: "[EPIC GOAL]\nbuild project"},
	}
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		if callCount == 1 {
			last := req.Messages[len(req.Messages)-1]
			// Consecutive user messages merge into one Claude user message
			// (roles must alternate) — the continuation text block is the
			// LAST block of the merged message.
			if last.Role != "user" || len(last.Content) < 2 {
				t.Fatalf("last message = %+v, want merged user message with continuation block", last)
			}
			lastBlock := last.Content[len(last.Content)-1]
			if lastBlock.Type != "text" || lastBlock.Text != "Now add tests" {
				t.Errorf("last block = %+v, want verbatim continuation \"Now add tests\"", lastBlock)
			}
		}
		claudeTextStream(w, "ok", "end_turn")
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
		MaxIterations: 5,
	}
	if _, err := ag.Resume("Now add tests", history); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
	if callCount < 1 {
		t.Fatal("expected at least 1 API call")
	}
}

// TestResume_NoTaskStartSearchEnforcement verifies the resumed run does NOT
// inject the one-shot task-start memory_search checkpoint (it would corrupt
// the recovered context with a fresh-search instruction).
func TestResume_NoTaskStartSearchEnforcement(t *testing.T) {
	history := []deepseek.Message{
		{Role: "user", Content: "[EPIC GOAL]\nrefactor module"},
	}
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		if callCount == 1 {
			// The taskStartSearchMessage must NOT appear anywhere.
			for _, m := range req.Messages {
				for _, b := range m.Content {
					if strings.Contains(fmt.Sprint(b.Text), "task-start checkpoint") ||
						strings.Contains(fmt.Sprint(b.Text), "closed-loop ENTRY") {
						t.Errorf("resumed request contains task-start search enforcement: %q", b.Text)
					}
				}
			}
		}
		claudeTextStream(w, "ok", "end_turn")
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
		MaxIterations: 5,
	}
	if _, err := ag.Resume("", history); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
}

// TestResume_DanglingToolUse verifies a resumed context whose last assistant
// turn carried a tool_use with no result (the interruption happened between
// the tool round and its result) still builds a request the API ACCEPTS.
//
// ← FLIPPED ASSERTION (2026-09-22, epic tool-pairing). This test used to
// document the OPPOSITE: "a dangling tool_use is simply carried in history and
// the API-side conversion keeps it", with the expectation that the last
// message stays the bare continuation text. That encoded the bug. The Claude
// API rejects an unanswered tool_use deterministically — HTTP 400 "`tool_use`
// ids were found without `tool_result` blocks immediately after" (measured:
// /home/user/.gopi/dumps/20260922-040822.dump.json, where an unanswered head tool_use
// killed a live run at its hand-off and then every resume of it). So the
// harness now REPAIRS the dangling call with a synthesized tool_result before
// the continuation, and the last message is the continuation arriving after
// that result. What the test fixes in place is the property that actually
// matters: the resumed wire carries a tool_result for every tool_use.
func TestResume_DanglingToolUse(t *testing.T) {
	history := []deepseek.Message{
		{Role: "user", Content: "[EPIC GOAL]\ndo a thing"},
		{Role: "assistant", Content: "", ToolCalls: []deepseek.ToolCall{
			{ID: "call_9", Name: "bash", Arguments: map[string]interface{}{"command": "echo hi"}},
		}},
	}
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		req := claudeRequestFrom(t, r)
		w.Header().Set("Content-Type", "text/event-stream")
		if callCount == 1 {
			// The API's pairing rule — the assertion this test lacked while it
			// "documented" the rejected shape.
			for i := range req.Messages {
				m := &req.Messages[i]
				if m.Role != "assistant" {
					continue
				}
				for _, b := range m.Content {
					if b.Type != "tool_use" || b.ID == "" {
						continue
					}
					answered := false
					if i+1 < len(req.Messages) && req.Messages[i+1].Role == "user" {
						for _, nb := range req.Messages[i+1].Content {
							if nb.Type == "tool_result" && nb.ToolUseID == b.ID {
								answered = true
							}
						}
					}
					if !answered {
						t.Errorf("resumed wire carries unanswered tool_use %q at messages.%d — the API rejects this with HTTP 400", b.ID, i)
					}
				}
			}
			// The dangling call must be the one that got a synthesized result.
			sawRepair := false
			for _, m := range req.Messages {
				for _, b := range m.Content {
					if b.Type == "tool_result" && b.ToolUseID == "call_9" {
						sawRepair = true
					}
				}
			}
			if !sawRepair {
				t.Errorf("dangling tool_use call_9 was not repaired: %+v", req.Messages)
			}
			// The continuation is still delivered (after the repaired result).
			last := req.Messages[len(req.Messages)-1]
			if last.Role != "user" {
				t.Fatalf("last message role = %q, want user", last.Role)
			}
			sawContinuation := false
			for _, b := range last.Content {
				if strings.Contains(b.Text, "Continue from where you left off") {
					sawContinuation = true
				}
			}
			if !sawContinuation {
				t.Errorf("last message = %+v, want the continuation", last)
			}
		}
		claudeTextStream(w, "ok", "end_turn")
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
		MaxIterations: 5,
	}
	if _, err := ag.Resume("", history); err != nil {
		t.Fatalf("Resume() error: %v", err)
	}
}
