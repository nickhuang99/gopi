package proxy

import (
	"encoding/json"
	"testing"
)

func TestNonStream_TextAndThinking(t *testing.T) {
	resp := &OpenAIChatResponse{
		ID:    "chatcmpl-1",
		Model: "glm-test",
		Choices: []OpenAIResponseChoice{{
			Index: 0,
			Message: OpenAIResponseMessage{
				Role:             "assistant",
				Content:          strPtr("hello world"),
				ReasoningContent: strPtr("internal reasoning"),
			},
			FinishReason: strPtr("stop"),
		}},
		Usage: &OpenAIUsage{PromptTokens: 15, CompletionTokens: 5, TotalTokens: 20},
	}
	got := ConvertNonStreamResponse("", "glm-test", resp, "")

	if got.ID != "chatcmpl-1" {
		t.Errorf("id: %s", got.ID)
	}
	if got.Role != "assistant" || got.Type != "message" {
		t.Errorf("role/type: %+v", got)
	}
	if len(got.Content) != 2 {
		t.Fatalf("content blocks=%d want 2 (thinking+text)", len(got.Content))
	}
	if got.Content[0].Type != "thinking" || got.Content[0].Thinking != "internal reasoning" {
		t.Errorf("block[0]: %+v", got.Content[0])
	}
	if got.Content[1].Type != "text" || got.Content[1].Text != "hello world" {
		t.Errorf("block[1]: %+v", got.Content[1])
	}
	if got.StopReason == nil || *got.StopReason != "end_turn" {
		t.Errorf("stop_reason: %v", got.StopReason)
	}
	if got.Usage == nil || got.Usage.InputTokens != 15 || got.Usage.OutputTokens != 5 {
		t.Errorf("usage: %+v", got.Usage)
	}
}

func TestNonStream_CacheReadInputTokens(t *testing.T) {
	resp := &OpenAIChatResponse{
		ID:    "chatcmpl-cache",
		Model: "glm-test",
		Choices: []OpenAIResponseChoice{{
			Index:        0,
			Message:      OpenAIResponseMessage{Role: "assistant", Content: strPtr("ok")},
			FinishReason: strPtr("stop"),
		}},
		Usage: &OpenAIUsage{
			PromptTokens:        30,
			CompletionTokens:    8,
			TotalTokens:         38,
			PromptTokensDetails: &OpenAIPromptTokensDetails{CachedTokens: 24},
		},
	}
	got := ConvertNonStreamResponse("", "glm-test", resp, "")
	if got.Usage == nil {
		t.Fatalf("usage missing")
	}
	if got.Usage.InputTokens != 30 || got.Usage.OutputTokens != 8 {
		t.Errorf("input/output: %+v", got.Usage)
	}
	if got.Usage.CacheReadInputTokens != 24 {
		t.Errorf("cache_read_input_tokens: %+v", got.Usage)
	}
}

func TestNonStream_ToolCalls(t *testing.T) {
	resp := &OpenAIChatResponse{
		ID:    "chatcmpl-2",
		Model: "glm-test",
		Choices: []OpenAIResponseChoice{{
			Index: 0,
			Message: OpenAIResponseMessage{
				Role:    "assistant",
				Content: strPtr("going to call"),
				ToolCalls: []OpenAIToolCall{{
					ID:   "call_xyz",
					Type: "function",
					Function: OpenAIFunctionCall{
						Name:      "get_weather",
						Arguments: `{"city":"Beijing"}`,
					},
				}},
			},
			FinishReason: strPtr("tool_calls"),
		}},
	}
	got := ConvertNonStreamResponse("", "glm-test", resp, "")
	var toolBlock *ContentBlock
	for i := range got.Content {
		if got.Content[i].Type == "tool_use" {
			toolBlock = &got.Content[i]
		}
	}
	if toolBlock == nil {
		t.Fatalf("no tool_use block: %+v", got.Content)
	}
	if toolBlock.ID != "call_xyz" || toolBlock.Name != "get_weather" {
		t.Errorf("tool block: %+v", toolBlock)
	}
	var args map[string]any
	if err := json.Unmarshal(toolBlock.Input, &args); err != nil {
		t.Fatalf("tool input not valid JSON: %v", err)
	}
	if args["city"] != "Beijing" {
		t.Errorf("tool args: %v", args)
	}
	if got.StopReason == nil || *got.StopReason != "tool_use" {
		t.Errorf("stop_reason: %v", got.StopReason)
	}
}

func TestNonStream_EmptyContent(t *testing.T) {
	resp := &OpenAIChatResponse{
		ID:    "chatcmpl-3",
		Model: "glm-test",
		Choices: []OpenAIResponseChoice{{
			Index:        0,
			Message:      OpenAIResponseMessage{Role: "assistant"},
			FinishReason: strPtr("stop"),
		}},
	}
	got := ConvertNonStreamResponse("", "glm-test", resp, "")
	if len(got.Content) != 0 {
		t.Errorf("expected empty content, got %+v", got.Content)
	}
	if *got.StopReason != "end_turn" {
		t.Errorf("stop_reason=%v", got.StopReason)
	}
}

func strPtr(s string) *string { return &s }
