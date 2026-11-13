package proxy

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
)

// translateStream runs the translator over a slice of OpenAI chunk JSON strings
// and returns the raw Anthropic SSE text emitted.
func translateStream(t *testing.T, chunks []string) string {
	t.Helper()
	input := ""
	for _, c := range chunks {
		input += "data: " + c + "\n\n"
	}
	input += "data: [DONE]\n\n"
	var out bytes.Buffer
	acc := newStreamTranslator("test-msg", "glm-test")
	if err := collect(acc, strings.NewReader(input), &out); err != nil {
		t.Fatalf("translateStream: %v", err)
	}
	return out.String()
}

// collect drives a translator with the OpenAI requests from r. message_start is
// emitted lazily by Feed on the first chunk.
func collect(acc *streamTranslator, r io.Reader, w io.Writer) error {
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(strings.TrimSpace(line), "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "data:"))
		if data == "[DONE]" {
			break
		}
		var chunk OpenAIChatChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			continue
		}
		if err := acc.Feed(w, chunk); err != nil {
			return err
		}
	}
	return sc.Err()
}

// parseSSE splits raw Anthropic SSE text into events: {type -> []data json}.
func parseSSE(t *testing.T, raw string) []struct {
	Type string
	Data string
} {
	t.Helper()
	var out []struct{ Type, Data string }
	sc := bufio.NewScanner(strings.NewReader(raw))
	var curType string
	var curData []string
	flush := func() {
		if curType != "" {
			out = append(out, struct{ Type, Data string }{curType, strings.Join(curData, "\n")})
		}
		curType = ""
		curData = nil
	}
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			flush()
			curType = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			curData = append(curData, strings.TrimPrefix(line, "data: "))
		case line == "":
			flush()
		}
	}
	flush()
	return out
}

func TestStream_TextOrdering(t *testing.T) {
	raw := translateStream(t, []string{
		`{"id":"c1","model":"glm-test","choices":[{"index":0,"delta":{"role":"assistant","content":"Hel"}}]}`,
		`{"id":"c1","model":"glm-test","choices":[{"index":0,"delta":{"content":"lo "}}]}`,
		`{"id":"c1","model":"glm-test","choices":[{"index":0,"delta":{"content":"world"},"finish_reason":"stop"}]}`,
	})
	evs := parseSSE(t, raw)
	// Expected: message_start, content_block_start(text), 3x content_block_delta, content_block_stop, message_delta, message_stop
	types := []string{}
	for _, e := range evs {
		types = append(types, e.Type)
	}
	want := []string{"message_start", "content_block_start", "content_block_delta", "content_block_delta", "content_block_delta", "content_block_stop", "message_delta", "message_stop"}
	if len(types) != len(want) {
		t.Fatalf("event types=%v want %v", types, want)
	}
	for i := range want {
		if types[i] != want[i] {
			t.Errorf("event[%d]=%q want %q", i, types[i], want[i])
		}
	}
	// message_start fields
	ms := evs[0].Data
	var sm AnthropicEvent
	if err := json.Unmarshal([]byte(ms), &sm); err != nil {
		t.Fatalf("message_start parse: %v", err)
	}
	if sm.Message == nil || sm.Message.ID != "c1" || sm.Message.Model != "glm-test" || sm.Message.Role != "assistant" {
		t.Errorf("message_start: %+v", sm.Message)
	}
	// content_block_start text
	var cbs AnthropicEvent
	json.Unmarshal([]byte(evs[1].Data), &cbs)
	if cbs.ContentBlock == nil || cbs.ContentBlock.Type != "text" {
		t.Errorf("content_block_start: %+v", cbs.ContentBlock)
	}
	// deltas concatenate
	concat := ""
	for _, e := range evs[2:5] {
		var d AnthropicEvent
		json.Unmarshal([]byte(e.Data), &d)
		if d.Delta != nil {
			if td, ok := d.Delta.(map[string]any); ok {
				if txt, ok := td["text"].(string); ok {
					concat += txt
				}
			}
		}
	}
	if concat != "Hello world" {
		t.Errorf("concatenated text=%q want %q", concat, "Hello world")
	}
	// message_delta stop_reason
	var md AnthropicEvent
	json.Unmarshal([]byte(evs[6].Data), &md)
	dm, ok := md.Delta.(map[string]any)
	if !ok || dm["stop_reason"] != "end_turn" {
		t.Errorf("message_delta stop_reason=%v", md.Delta)
	}
}

func TestStream_ThinkingBlock(t *testing.T) {
	raw := translateStream(t, []string{
		`{"id":"c2","choices":[{"index":0,"delta":{"reasoning_content":"think step1 "}}]}`,
		`{"id":"c2","choices":[{"index":0,"delta":{"reasoning_content":"think step2","content":""}}]}`,
		`{"id":"c2","choices":[{"index":0,"delta":{"content":"answer"},"finish_reason":"stop"}]}`,
	})
	evs := parseSSE(t, raw)
	// The translator must emit the thinking block completely (start + deltas +
	// stop) before opening the text block. Verify by scanning the event stream:
	// text block start may only appear after a content_block_stop was emitted.
	stopCount := 0
	thinkingStarted := false
	textStarted := false
	thoughtText := ""
	for _, e := range evs {
		switch e.Type {
		case "content_block_start":
			var ev AnthropicEvent
			json.Unmarshal([]byte(e.Data), &ev)
			switch ev.ContentBlock.Type {
			case "thinking":
				thinkingStarted = true
			case "text":
				textStarted = true
				if thinkingStarted && stopCount == 0 {
					t.Errorf("text block opened before thinking block was closed")
				}
			}
		case "content_block_stop":
			stopCount++
		case "content_block_delta":
			var ev AnthropicEvent
			json.Unmarshal([]byte(e.Data), &ev)
			if dm, ok := ev.Delta.(map[string]any); ok {
				if th, ok := dm["thinking"].(string); ok {
					thoughtText += th
				}
			}
		}
	}
	if !thinkingStarted || thoughtText != "think step1 think step2" {
		t.Errorf("thinking deltas=%q (started=%v)", thoughtText, thinkingStarted)
	}
	if !textStarted {
		t.Errorf("expected a text block")
	}
}

func TestStream_ToolCallIncrement(t *testing.T) {
	// Simulate GLM-streamed tool call: id+name on first frag, args in fragments.
	raw := translateStream(t, []string{
		`{"id":"c3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_abc","type":"function","function":{"name":"read","arguments":""}}]}}]}`,
		`{"id":"c3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"path\":"}}]}}]}`,
		`{"id":"c3","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"/tmp/x\"}"}}]}}]}`,
		`{"id":"c3","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	})
	evs := parseSSE(t, raw)

	// Find the tool_use content_block_start and the input_json_delta deltas.
	var toolStart *AnthropicEvent
	argsConcat := ""
	for _, e := range evs {
		switch e.Type {
		case "content_block_start":
			var ev AnthropicEvent
			json.Unmarshal([]byte(e.Data), &ev)
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				toolStart = &ev
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
	if toolStart == nil {
		t.Fatalf("no tool_use block start")
	}
	blk := toolStart.ContentBlock
	if blk.ID != "call_abc" || blk.Name != "read" {
		t.Errorf("tool block id/name: %+v", blk)
	}
	if argsConcat != `{"path":"/tmp/x"}` {
		t.Errorf("concatenated arguments=%q", argsConcat)
	}
	// stop_reason should map tool_calls -> tool_use
	var md AnthropicEvent
	for _, e := range evs {
		if e.Type == "message_delta" {
			json.Unmarshal([]byte(e.Data), &md)
		}
	}
	dm, _ := md.Delta.(map[string]any)
	if dm["stop_reason"] != "tool_use" {
		t.Errorf("stop_reason=%v want tool_use", dm["stop_reason"])
	}
}

func TestStream_MultipleToolCalls(t *testing.T) {
	raw := translateStream(t, []string{
		`{"id":"c4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_0","function":{"name":"read"}}]}}]}`,
		`{"id":"c4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"id":"call_1","function":{"name":"write"}}]}}]}`,
		`{"id":"c4","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{}"}}]}}]}`,
		`{"id":"c4","choices":[{"index":0,"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{x}"}}]}}]}`,
		`{"id":"c4","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`,
	})
	evs := parseSSE(t, raw)
	// Should produce TWO content_block_start of type tool_use
	starts := 0
	for _, e := range evs {
		if e.Type == "content_block_start" {
			var ev AnthropicEvent
			json.Unmarshal([]byte(e.Data), &ev)
			if ev.ContentBlock != nil && ev.ContentBlock.Type == "tool_use" {
				starts++
			}
		}
	}
	if starts != 2 {
		t.Errorf("tool_use block starts=%d want 2", starts)
	}
}

func TestStream_FinishWithoutUsage(t *testing.T) {
	raw := translateStream(t, []string{
		`{"id":"c5","choices":[{"index":0,"delta":{"content":"hi"},"finish_reason":"length"}]}`,
	})
	evs := parseSSE(t, raw)
	var md AnthropicEvent
	for _, e := range evs {
		if e.Type == "message_delta" {
			json.Unmarshal([]byte(e.Data), &md)
		}
	}
	dm, _ := md.Delta.(map[string]any)
	if dm["stop_reason"] != "max_tokens" {
		t.Errorf("length finish_reason -> stop_reason=%v want max_tokens", dm["stop_reason"])
	}
	if md.Usage != nil {
		t.Errorf("usage should be nil when upstream omits it, got %+v", md.Usage)
	}
}

func TestStream_UsagePassthrough(t *testing.T) {
	raw := translateStream(t, []string{
		`{"id":"c6","choices":[{"index":0,"delta":{"content":"x"},"finish_reason":"stop"}],"usage":{"prompt_tokens":25,"completion_tokens":10,"total_tokens":35,"prompt_tokens_details":{"cached_tokens":20}}}`,
	})
	evs := parseSSE(t, raw)
	var md AnthropicEvent
	for _, e := range evs {
		if e.Type == "message_delta" {
			json.Unmarshal([]byte(e.Data), &md)
		}
	}
	if md.Usage == nil || md.Usage.InputTokens != 25 || md.Usage.OutputTokens != 10 {
		t.Errorf("usage mapping: %+v", md.Usage)
	}
	if md.Usage == nil || md.Usage.CacheReadInputTokens != 20 {
		t.Errorf("cache_read_input_tokens mapping: %+v", md.Usage)
	}
}
