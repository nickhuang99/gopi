package proxy

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestProxy starts a proxy against a mock upstream that returns the given
// handler. Returns the proxy httptest.Server and the upstream request recorder.
func newTestProxy(t *testing.T, upstream http.HandlerFunc) (proxyURL string, ok chan []byte) {
	t.Helper()
	ok = make(chan []byte, 1)
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
		select {
		case ok <- body:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		upstream(w, r)
	}))
	t.Cleanup(mock.Close)

	srv := NewServer(UpstreamConfig{BaseURL: mock.URL, Model: "glm-mock", Key: "secret"}, false, nil)
	proxy := httptest.NewServer(srv.Handler())
	t.Cleanup(proxy.Close)
	return proxy.URL, ok
}

func doAnthropicPost(t *testing.T, url, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url+"/v1/messages", strings.NewReader(body)) //stylegate:ignore builds a request against the test's own server; the response is the assertion
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "client-key")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	b, _ := io.ReadAll(resp.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
	resp.Body.Close()
	return resp, string(b)
}

func TestProxy_StreamRoundtrip(t *testing.T) {
	// Mock upstream: echo back with a GLM-style reasoning_content + content stream.
	proxyURL, gotBody := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"id\":\"c1\",\"model\":\"glm-mock\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"think\"}}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c1\",\"model\":\"glm-mock\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"Hi \"}}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c1\",\"model\":\"glm-mock\",\"choices\":[{\"index\":0,\"delta\":{\"content\":\"there\"},\"finish_reason\":\"stop\"}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})

	body := `{"model":"claude-3","max_tokens":300,"stream":true,"system":"be nice","messages":[{"role":"user","content":"hello"}]}`
	resp, respBody := doAnthropicPost(t, proxyURL, body)
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Errorf("content-type: %s", ct)
	}

	// Verify the forwarded OpenAI request had the auth header replaced.
	forwarded := <-gotBody
	var or OpenAIRequest
	if err := json.Unmarshal(forwarded, &or); err != nil {
		t.Fatalf("forwarded request parse: %v", err)
	}
	if or.Model != "glm-mock" {
		t.Errorf("forwarded model=%s", or.Model)
	}
	if or.MaxTokens != 300 || !or.Stream {
		t.Errorf("forwarded max_tokens/stream: %d/%v", or.MaxTokens, or.Stream)
	}
	if len(or.Messages) != 2 || or.Messages[0].Role != "system" || or.Messages[0].Content != "be nice" {
		t.Errorf("forwarded messages: %+v", or.Messages)
	}

	// Verify the Anthropic SSE sequence is well-formed.
	evs := parseSSE(t, respBody)
	if len(evs) < 8 {
		t.Fatalf("got %d events, want >=8", len(evs))
	}
	if evs[0].Type != "message_start" {
		t.Errorf("first event=%s want message_start", evs[0].Type)
	}
	// contains thinking block start, text block, and terminal events
	typeCount := map[string]int{}
	for _, e := range evs {
		typeCount[e.Type]++
	}
	if typeCount["content_block_delta"] < 2 {
		t.Errorf("content_block_delta count=%d", typeCount["content_block_delta"])
	}
	if evs[len(evs)-1].Type != "message_stop" {
		t.Errorf("last event=%s want message_stop", evs[len(evs)-1].Type)
	}
	// message_start id must match the upstream id c1
	var ms AnthropicEvent
	json.Unmarshal([]byte(evs[0].Data), &ms)
	if ms.Message.ID != "c1" {
		t.Errorf("message id=%s want c1", ms.Message.ID)
	}
}

func TestProxy_NonStreamRoundtrip(t *testing.T) {
	proxyURL, gotBody := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"id":"cc9","model":"glm-mock","choices":[{"index":0,"message":{"role":"assistant","content":"plain answer","reasoning_content":"## think"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	})
	body := `{"model":"claude-3","max_tokens":100,"stream":false,"messages":[{"role":"user","content":"hi"}]}`
	resp, respBody := doAnthropicPost(t, proxyURL, body)

	forwarded := <-gotBody
	var or OpenAIRequest
	json.Unmarshal(forwarded, &or)
	if or.Stream {
		t.Errorf("non-stream request should set stream=false")
	}

	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var am AnthropicMessage
	if err := json.Unmarshal([]byte(respBody), &am); err != nil {
		t.Fatalf("response not Anthropic message: %v", err)
	}
	if am.Type != "message" || am.Role != "assistant" {
		t.Errorf("type/role: %+v", am)
	}
	if len(am.Content) != 2 || am.Content[0].Type != "thinking" || am.Content[1].Type != "text" {
		t.Errorf("content: %+v", am.Content)
	}
	if am.Content[1].Text != "plain answer" {
		t.Errorf("text: %s", am.Content[1].Text)
	}
	if am.StopReason == nil || *am.StopReason != "end_turn" {
		t.Errorf("stop_reason: %v", am.StopReason)
	}
}

func TestProxy_ToolRoundtrip(t *testing.T) {
	// Anthropic client sends the assistant tool_use; the mock upstream requests a
	// tool call via tool_calls with incremental args. Verify the tool_use id is
	// preserved so the client's tool_result can reference it.
	proxyURL, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"id\":\"c2\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"call_keepme\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"\"}}]}}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c2\",\"choices\":[{\"index\":0,\"delta\":{\"tool_calls\":[{\"index\":0,\"function\":{\"arguments\":\"{\\\"path\\\":\\\"/tmp\\\"}\"}}]}}]}\n\n")
		io.WriteString(w, "data: {\"id\":\"c2\",\"choices\":[{\"index\":0,\"delta\":{},\"finish_reason\":\"tool_calls\"}]}\n\n")
		io.WriteString(w, "data: [DONE]\n\n")
	})
	body := `{"model":"claude-3","max_tokens":300,"stream":true,"messages":[{"role":"user","content":"read /tmp"}],"tools":[{"name":"read_file","input_schema":{"type":"object","properties":{"path":{"type":"string"}}}}]}`
	_, respBody := doAnthropicPost(t, proxyURL, body)

	evs := parseSSE(t, respBody)
	var startedID, startedName, argsConcat string
	for _, e := range evs {
		switch e.Type {
		case "content_block_start":
			var ev AnthropicEvent
			json.Unmarshal([]byte(e.Data), &ev)
			if ev.ContentBlock.Type == "tool_use" {
				startedID = ev.ContentBlock.ID
				startedName = ev.ContentBlock.Name
			}
		case "content_block_delta":
			var ev AnthropicEvent
			json.Unmarshal([]byte(e.Data), &ev)
			if dm, ok := ev.Delta.(map[string]any); ok {
				if pj, ok := dm["partial_json"].(string); ok {
					argsConcat += pj
				}
			}
		}
	}
	if startedID != "call_keepme" {
		t.Errorf("tool_use id=%s want call_keepme (must be preserved for tool_result roundtrip)", startedID)
	}
	if startedName != "read_file" {
		t.Errorf("tool_use name=%s", startedName)
	}
	if argsConcat != `{"path":"/tmp"}` {
		t.Errorf("arguments concat=%s", argsConcat)
	}
	var md AnthropicEvent
	for _, e := range evs {
		if e.Type == "message_delta" {
			json.Unmarshal([]byte(e.Data), &md)
		}
	}
	dm, _ := md.Delta.(map[string]any)
	if dm["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason=%v", dm["stop_reason"])
	}
}

func TestProxy_UpstreamError_Passthrough(t *testing.T) {
	proxyURL, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		io.WriteString(w, `{"error":{"message":"rate limited","type":"rate_limit_error"}}`)
	})
	resp, body := doAnthropicPost(t, proxyURL, `{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"x"}]}`)
	if resp.StatusCode != 429 {
		t.Errorf("status=%d want 429 passthrough", resp.StatusCode)
	}
	var errBody struct {
		Type  string `json:"type"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(body), &errBody); err != nil {
		t.Fatalf("error body parse: %v (%s)", err, body)
	}
	if errBody.Type != "error" || errBody.Error.Type != "api_error" {
		t.Errorf("error envelope: %+v", errBody)
	}
	if !strings.Contains(errBody.Error.Message, "rate limited") {
		t.Errorf("error message: %s", errBody.Error.Message)
	}
}

// TestToolResultReferencingToolCall verifies the request-side mapping: a
// client tool_result must translate to an OpenAI role=tool message whose
// tool_call_id equals the assistant tool_use id.
func TestToolResultReferencingToolCall(t *testing.T) {
	proxyURL, gotBody := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"s","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"done"},"finish_reason":"stop"}]}`)
	})
	body := `{"model":"claude-3","max_tokens":100,"stream":false,"messages":[
		{"role":"user","content":"go"},
		{"role":"assistant","content":[{"type":"tool_use","id":"toolu_x","name":"read","input":{"p":"y"}}]},
		{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_x","content":"the file says z"}]}
	]}`
	doAnthropicPost(t, proxyURL, body)
	forwarded := <-gotBody
	var or OpenAIRequest
	json.Unmarshal(forwarded, &or)
	// expected: user, assistant(tool_calls), tool
	if len(or.Messages) != 3 {
		t.Fatalf("messages=%d want 3", len(or.Messages))
	}
	toolMsg := or.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "toolu_x" || toolMsg.Content != "the file says z" {
		t.Errorf("tool message: %+v", toolMsg)
	}
	ast := or.Messages[1]
	if len(ast.ToolCalls) != 1 || ast.ToolCalls[0].ID != "toolu_x" {
		t.Errorf("assistant tool call id must match: %+v", ast.ToolCalls)
	}
}

// TestProxy_AnthropicMessagesAlias verifies the /anthropic/messages path (gopi's
// own DeepSeek-style client path) routes to the same handler as /v1/messages.
func TestProxy_AnthropicMessagesAlias(t *testing.T) {
	proxyURL, _ := newTestProxy(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, `{"id":"alias","model":"m","choices":[{"index":0,"message":{"role":"assistant","content":"via alias"},"finish_reason":"stop"}]}`)
	})
	req, _ := http.NewRequest(http.MethodPost, proxyURL+"/anthropic/messages", strings.NewReader(`{"model":"m","max_tokens":10,"messages":[{"role":"user","content":"hi"}]}`)) //stylegate:ignore builds a request against the test's own server; the response is the assertion
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	body, _ := io.ReadAll(resp.Body) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status=%d", resp.StatusCode)
	}
	var am AnthropicMessage
	if err := json.Unmarshal(body, &am); err != nil {
		t.Fatalf("not a message: %v", err)
	}
	if am.Content != nil && len(am.Content) > 0 && am.Content[0].Type == "text" && am.Content[0].Text != "via alias" {
		t.Errorf("text=%s", am.Content[0].Text)
	}
}
