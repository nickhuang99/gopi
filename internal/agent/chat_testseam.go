package agent

// chat_testseam.go — the agent-package seams a SIBLING package's test needs to
// drive a real chat round. Go's test files are package-private, so a fixture
// living outside internal/agent (it must: gopilog imports agent, so an
// agent-package test importing gopilog is an import cycle) has no way to serve a
// Claude SSE stream. These three exports are that way in — they are seams, not
// product surface: nothing in the product calls them, and they carry no
// behaviour of their own (writeClaudeSSE/claudeTextStream/claudeToolUseStream
// are the SAME helpers internal/agent's own tests use, so a sibling fixture
// cannot drift from what the product's tests consider a valid Claude stream).

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// WriteClaudeSSEEvent writes one Claude SSE event (event: + data: lines) and
// flushes it.
func WriteClaudeSSEEvent(w http.ResponseWriter, eventType, payloadJSON string) {
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, payloadJSON)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

// WriteClaudeTextStream writes a minimal Claude SSE text response ending with
// the given stop_reason — one assistant text block carrying text.
func WriteClaudeTextStream(w http.ResponseWriter, text, stopReason string) {
	textJSON, _ := json.Marshal(text) //stylegate:ignore marshalling a value built in-process cannot fail
	WriteClaudeSSEEvent(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	WriteClaudeSSEEvent(w, "content_block_start", "{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}")
	WriteClaudeSSEEvent(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":%s}}", textJSON))
	WriteClaudeSSEEvent(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	WriteClaudeSSEEvent(w, "message_delta", fmt.Sprintf("{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":%q,\"stop_sequence\":null}}", stopReason))
	WriteClaudeSSEEvent(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// WriteClaudeToolUseStream writes a minimal Claude SSE response whose single
// content block is a tool_use for name with args, ending with stop_reason
// tool_use.
func WriteClaudeToolUseStream(w http.ResponseWriter, name string, args map[string]interface{}) {
	argsJSON, _ := json.Marshal(args)                //stylegate:ignore marshalling a value built in-process cannot fail
	argsJSONStr, _ := json.Marshal(string(argsJSON)) //stylegate:ignore marshalling a value built in-process cannot fail
	WriteClaudeSSEEvent(w, "message_start", "{\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"usage\":{\"input_tokens\":10,\"output_tokens\":0}}}")
	WriteClaudeSSEEvent(w, "content_block_start", fmt.Sprintf("{\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"tool_use\",\"id\":\"call_1\",\"name\":%q,\"input\":{}}}", name))
	WriteClaudeSSEEvent(w, "content_block_delta", fmt.Sprintf("{\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"input_json_delta\",\"partial_json\":%s}}", argsJSONStr))
	WriteClaudeSSEEvent(w, "content_block_stop", "{\"type\":\"content_block_stop\",\"index\":0}")
	WriteClaudeSSEEvent(w, "message_delta", "{\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"tool_use\",\"stop_sequence\":null},\"usage\":{\"input_tokens\":10,\"output_tokens\":5}}")
	WriteClaudeSSEEvent(w, "message_stop", "{\"type\":\"message_stop\"}")
}

// NewChatOpsTestAgent builds a tool-enabled chat agent pointed at a fake model
// server, writing its session events into sl. It is the fixture entry point for
// a sibling package's chat-writer test: the profile it resolves (session dir,
// memory DB, settings) comes from the CALLER's environment, so the caller owns
// the isolation and this constructor invents none.
//
// The agent is the same shape internal/agent's own chat tests build
// (chatOpsAgent): the ordinary tool surface, the chat system prompt, an empty
// context and a discarded logger.
func NewChatOpsTestAgent(t interface {
	Helper()
	Fatalf(string, ...interface{})
}, baseURL string, sl *SessionLogger) *Agent {
	t.Helper()
	ag := &Agent{
		Client: &deepseek.Client{
			BaseURL:    baseURL,
			APIKey:     "test-key",
			HTTPClient: &http.Client{Transport: &http.Transport{}},
		},
		Ctx:           &Context{},
		Model:         "deepseek-v4-pro",
		MaxIterations: 5,
		SessionLog:    sl,
	}
	ag.SystemPrompt = assembleSystemPrompt()
	return ag
}
