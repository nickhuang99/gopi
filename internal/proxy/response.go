package proxy

import (
	"encoding/json"
	"strings"
)

// ConvertNonStreamResponse translates a non-streaming OpenAI Chat Completions
// response into an Anthropic Messages response body:
//
//	{"id":..,"type":"message","role":"assistant","model":..,"content":[blocks],"stop_reason":..,"usage":{..}}
//
// reasoning_content -> a thinking block; content -> text block; tool_calls ->
// tool_use blocks. The id is reused so downstream tool_result references match.
func ConvertNonStreamResponse(openAIID, model string, resp *OpenAIChatResponse, msgID string) *AnthropicMessage {
	if len(resp.Choices) == 0 {
		return &AnthropicMessage{
			ID:    msgID,
			Type:  "message",
			Role:  "assistant",
			Model: model,
		}
	}
	choice := resp.Choices[0]
	m := choice.Message

	if msgID == "" {
		if resp.ID != "" {
			msgID = resp.ID
		} else if openAIID != "" {
			msgID = openAIID
		} else {
			msgID = "msg_default"
		}
	}
	if model == "" {
		model = resp.Model
	}

	am := &AnthropicMessage{
		ID:      msgID,
		Type:    "message",
		Role:    "assistant",
		Model:   model,
		Content: []ContentBlock{},
	}

	// thinking block from reasoning_content.
	if m.ReasoningContent != nil && *m.ReasoningContent != "" {
		am.Content = append(am.Content, ContentBlock{Type: "thinking", Thinking: *m.ReasoningContent})
	}

	// text block from content.
	if m.Content != nil && *m.Content != "" {
		am.Content = append(am.Content, ContentBlock{Type: "text", Text: *m.Content})
	}

	// tool_use blocks from tool_calls.
	for _, tc := range m.ToolCalls {
		am.Content = append(am.Content, ContentBlock{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Function.Name,
			Input: json.RawMessage(validJSONOrEmpty(tc.Function.Arguments)),
		})
	}

	// stop_reason.
	if choice.FinishReason != nil {
		sr := mapFinishReason(*choice.FinishReason)
		am.StopReason = &sr
	} else {
		sr := "end_turn"
		am.StopReason = &sr
	}

	// usage.
	if resp.Usage != nil {
		au := AnthropicUsage{
			InputTokens:  resp.Usage.PromptTokens,
			OutputTokens: resp.Usage.CompletionTokens,
		}
		if resp.Usage.PromptTokensDetails != nil {
			au.CacheReadInputTokens = resp.Usage.PromptTokensDetails.CachedTokens
		}
		am.Usage = &au
	}
	return am
}

// validJSONOrEmpty returns s if it parses as JSON, else an empty object,
// guarding against a truncated arguments string.
func validJSONOrEmpty(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}
	var v any
	if json.Unmarshal([]byte(s), &v) == nil {
		return s
	}
	return "{}"
}
