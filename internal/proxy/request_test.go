package proxy

import (
	"encoding/json"
	"testing"
)

func mustReq(t *testing.T, body string) *AnthropicRequest {
	t.Helper()
	var ar AnthropicRequest
	if err := json.Unmarshal([]byte(body), &ar); err != nil {
		t.Fatalf("unmarshal request: %v", err)
	}
	return &ar
}

func TestConvertRequest_SystemAndMessages(t *testing.T) {
	ar := mustReq(t, `{
		"model": "claude-3", "max_tokens": 4096, "stream": true,
		"system": [{"type":"text","text":"You are a helper."}],
		"messages": [
			{"role":"user","content":"hello"},
			{"role":"assistant","content":[{"type":"text","text":"hi"}]},
			{"role":"user","content":"world"}
		]
	}`)
	or := convertRequest(ar, "glm-5.3-flash")

	if or.Model != "glm-5.3-flash" {
		t.Errorf("model override: got %q want glm-5.3-flash", or.Model)
	}
	if or.MaxTokens != 4096 || !or.Stream {
		t.Errorf("max_tokens/stream: got %d/%v", or.MaxTokens, or.Stream)
	}
	if len(or.Messages) != 4 {
		t.Fatalf("messages len=%d want 4 (system+3)", len(or.Messages))
	}
	if or.Messages[0].Role != "system" || or.Messages[0].Content != "You are a helper." {
		t.Errorf("system message: got %+v", or.Messages[0])
	}
	if or.Messages[1].Role != "user" || or.Messages[1].Content != "hello" {
		t.Errorf("user[1]: got %+v", or.Messages[1])
	}
	if or.Messages[2].Role != "assistant" || or.Messages[2].Content != "hi" {
		t.Errorf("assistant[2]: got %+v", or.Messages[2])
	}
	// system only when present
	ar2 := mustReq(t, `{"model":"m","max_tokens":1,"messages":[{"role":"user","content":"x"}]}`)
	or2 := convertRequest(ar2, "")
	if len(or2.Messages) != 1 || or2.Messages[0].Role != "user" {
		t.Errorf("no-system: got %+v", or2.Messages)
	}
}

func TestConvertRequest_ImageBlock(t *testing.T) {
	ar := mustReq(t, `{
		"model":"m","max_tokens":10,
		"messages":[{"role":"user","content":[
			{"type":"text","text":"what is this"},
			{"type":"image","source":{"type":"url","url":"https://x/y.png"}}
		]}]
	}`)
	or := convertRequest(ar, "")
	msg := or.Messages[0]
	parts, ok := msg.Content.([]OpenAIContentPart)
	if !ok {
		t.Fatalf("content not array: %T", msg.Content)
	}
	if len(parts) != 2 {
		t.Fatalf("parts len=%d want 2", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "what is this" {
		t.Errorf("parts[0]: %+v", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL == nil || parts[1].ImageURL.URL != "https://x/y.png" {
		t.Errorf("parts[1]: %+v", parts[1])
	}
}

func TestConvertRequest_AssistantToolUse(t *testing.T) {
	ar := mustReq(t, `{
		"model":"m","max_tokens":10,
		"messages":[{"role":"user","content":"go"},
		{"role":"assistant","content":[
			{"type":"text","text":"calling now"},
			{"type":"thinking","thinking":"let me plan"},
			{"type":"tool_use","id":"toolu_1","name":"read","input":{"path":"a.go"}}
		]}]
	}`)
	or := convertRequest(ar, "")
	am := or.Messages[1]
	if am.Content != "calling now" {
		t.Errorf("assistant content: got %#v", am.Content)
	}
	if len(am.ToolCalls) != 1 {
		t.Fatalf("tool_calls len=%d want 1", len(am.ToolCalls))
	}
	tc := am.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Function.Name != "read" {
		t.Errorf("tool call: %+v", tc)
	}
	if tc.Function.Arguments != `{"path":"a.go"}` {
		t.Errorf("arguments: got %s", tc.Function.Arguments)
	}
	// thinking dropped
}

func TestConvertRequest_ToolResult(t *testing.T) {
	ar := mustReq(t, "{\"model\":\"m\",\"max_tokens\":10,\"messages\":["+
		"{\"role\":\"user\",\"content\":\"go\"},"+
		"{\"role\":\"assistant\",\"content\":[{\"type\":\"tool_use\",\"id\":\"toolu_1\",\"name\":\"read\",\"input\":{\"p\":\"x\"}}]},"+
		"{\"role\":\"user\",\"content\":[{\"type\":\"tool_result\",\"tool_use_id\":\"toolu_1\",\"content\":\"file contents\"}]}"+
		"]}")
	or := convertRequest(ar, "")
	if len(or.Messages) != 3 {
		t.Fatalf("messages len=%d want 3", len(or.Messages))
	}
	last := or.Messages[2]
	if last.Role != "tool" || last.ToolCallID != "toolu_1" || last.Content != "file contents" {
		t.Errorf("tool message: %+v", last)
	}
	// tool_call_id must match the assistant tool call id
	if or.Messages[1].ToolCalls[0].ID != last.ToolCallID {
		t.Errorf("tool_call_id mismatch: assistant=%s result=%s", or.Messages[1].ToolCalls[0].ID, last.ToolCallID)
	}
}

func TestConvertRequest_ToolsAndToolChoice(t *testing.T) {
	ar := mustReq(t, `{
		"model":"m","max_tokens":10,
		"tools":[{"name":"get_weather","description":"weather","input_schema":{"type":"object","properties":{"city":{"type":"string"}},"required":["city"]}}],
		"tool_choice":{"type":"tool","name":"get_weather"},
		"messages":[{"role":"user","content":"hi"}]
	}`)
	or := convertRequest(ar, "")
	if len(or.Tools) != 1 {
		t.Fatalf("tools len=%d", len(or.Tools))
	}
	if or.Tools[0].Type != "function" {
		t.Errorf("tool type: %s", or.Tools[0].Type)
	}
	var fn struct {
		Name       string          `json:"name"`
		Parameters json.RawMessage `json:"parameters"`
	}
	if err := json.Unmarshal(or.Tools[0].Function, &fn); err != nil {
		t.Fatalf("tool function parse: %v", err)
	}
	if fn.Name != "get_weather" {
		t.Errorf("tool name: %s", fn.Name)
	}
	var params map[string]any
	json.Unmarshal(fn.Parameters, &params)
	if params["type"] != "object" {
		t.Errorf("parameters keep schema: %v", params)
	}
	tc, ok := or.ToolChoice.(map[string]any)
	if !ok {
		t.Fatalf("tool_choice type %T", or.ToolChoice)
	}
	if tc["type"] != "function" {
		t.Errorf("tool_choice.type: %v", tc["type"])
	}
	fnChoice, ok := tc["function"].(map[string]string)
	if !ok || fnChoice["name"] != "get_weather" {
		t.Errorf("tool_choice.function: %v", tc["function"])
	}
}

func TestConvertRequest_ToolChoiceVariants(t *testing.T) {
	// auto/any -> auto
	ar := mustReq(t, `{"model":"m","max_tokens":1,"tool_choice":{"type":"auto"},"messages":[{"role":"user","content":"x"}]}`)
	if tc := convertRequest(ar, "").ToolChoice; tc != "auto" {
		t.Errorf("auto -> %#v", tc)
	}
	ar = mustReq(t, `{"model":"m","max_tokens":1,"tool_choice":{"type":"any"},"messages":[{"role":"user","content":"x"}]}`)
	if tc := convertRequest(ar, "").ToolChoice; tc != "auto" {
		t.Errorf("any -> %#v", tc)
	}
	ar = mustReq(t, `{"model":"m","max_tokens":1,"tool_choice":{"type":"none"},"messages":[{"role":"user","content":"x"}]}`)
	if tc := convertRequest(ar, "").ToolChoice; tc != "none" {
		t.Errorf("none -> %#v", tc)
	}
}

func TestConvertRequest_TemperatureTopPStop(t *testing.T) {
	ar := mustReq(t, `{"model":"m","max_tokens":10,"temperature":0.7,"top_p":0.9,"stop_sequences":["END"],"messages":[{"role":"user","content":"x"}]}`)
	or := convertRequest(ar, "")
	if or.Temperature == nil || *or.Temperature != 0.7 {
		t.Errorf("temperature: %v", or.Temperature)
	}
	if or.TopP == nil || *or.TopP != 0.9 {
		t.Errorf("top_p: %v", or.TopP)
	}
	if len(or.Stop) != 1 || or.Stop[0] != "END" {
		t.Errorf("stop: %v", or.Stop)
	}
}

func TestConvertRequest_ThinkingDisabled(t *testing.T) {
	ar := mustReq(t, `{"model":"m","max_tokens":10,"thinking":{"type":"disabled"},"messages":[{"role":"user","content":"x"}]}`)
	or := convertRequest(ar, "")
	if or.Thinking == nil || or.Thinking["type"] != "disabled" {
		t.Errorf("thinking disabled mapping: %v", or.Thinking)
	}
	// enabled (default) should NOT force a mapping
	ar2 := mustReq(t, `{"model":"m","max_tokens":10,"thinking":{"type":"enabled","budget_tokens":1024},"messages":[{"role":"user","content":"x"}]}`)
	if or2 := convertRequest(ar2, ""); or2.Thinking != nil {
		t.Errorf("enabled thinking should not map, got %v", or2.Thinking)
	}
}
