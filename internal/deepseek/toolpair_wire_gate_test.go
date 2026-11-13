package deepseek

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// This file is the MASTER-RUNNABLE RED proof for the wire choke point: it uses
// only pre-existing production symbols (no repair/validate helper, no new
// constant), so it compiles and RUNS against master — and fails there,
// because master sends the unbalanced tool_use. Nothing here changes
// behaviour: the server applies the API's own rule to what it RECEIVED.
//
// The API's rule, verbatim from the death dump
// (/home/nick/.gopi/dumps/20260922-040822.dump.json, HTTP 400):
//
//	"messages.1: `tool_use` ids were found without `tool_result` blocks
//	 immediately after: call_00_ET_TBCTnLz8k12TGe2ZEVxl2105. Each `tool_use`
//	 block must have a corresponding `tool_result` block in the next message."
//
// Run it on master:  go test ./internal/deepseek/ -run TestClaudeChatStream_ServerNeverSeesUnbalancedToolUse -count=1

// serverSawUnbalancedToolUse applies the API's pairing rule to a request the
// server received, returning the offending id (or "" when the request is
// acceptable by the API's standard).
func serverSawUnbalancedToolUse(req *ClaudeRequest) string {
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
				return b.ID
			}
		}
	}
	return ""
}

// TestClaudeChatStream_ServerNeverSeesUnbalancedToolUse drives the real send
// path with the dumped request's shape (a head assistant tool_use whose
// tool_result is absent) and asserts against what the SERVER received.
//
// RED on master: the orphan reaches the wire. GREEN with the choke-point
// repair.
func TestClaudeChatStream_ServerNeverSeesUnbalancedToolUse(t *testing.T) {
	var got *ClaudeRequest
	acked := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ClaudeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode: %v", err)
		}
		got = &req
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":1,\"output_tokens\":0}}}\n\nevent: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"ok\"}}\n\nevent: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"input_tokens\":1,\"output_tokens\":1}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"))
		acked = true
	}))
	defer server.Close()

	client := &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: RetryConfig{MaxRetries: 0, InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
	}
	// The dumped shape, spelled with literals so this file has no dependency
	// on any symbol the fix introduces.
	req := &ClaudeRequest{
		Model:     "m",
		MaxTokens: 64,
		Messages: []ClaudeMessage{
			{Role: "assistant", Content: []ClaudeContentBlock{
				{Type: "tool_use", ID: "call_00_ET_TBCTnLz8k12TGe2ZEVxl2105", Name: "bash", Input: map[string]interface{}{"command": "ls"}},
			}},
			{Role: "user", Content: []ClaudeContentBlock{
				{Type: "text", Text: "continuing"},
			}},
		},
	}
	ch, err := client.ClaudeChatStream(context.Background(), req)
	if err != nil {
		// Fail-closed is also an acceptable outcome (nothing reached the wire).
		t.Logf("send refused (fail-closed): %v", err)
		return
	}
	for range ch {
	}
	if !acked || got == nil {
		t.Fatal("server received no request")
	}
	if id := serverSawUnbalancedToolUse(got); id != "" {
		t.Errorf("server received an unanswered tool_use %q — the API answers exactly this with HTTP 400 \"`tool_use` ids were found without `tool_result` blocks immediately after\"", id)
	}
}
