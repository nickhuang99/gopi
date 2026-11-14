package proxy

import (
	"encoding/json"
	"fmt"

	"github.com/nickhuang99/gopi/internal/thinkmode"
)

// convertRequest translates an Anthropic /v1/messages request into the
// OpenAI Chat Completions request shape for the upstream. The model override
// (if non-empty) replaces AnthropicRequest.Model — the caller supplies the
// --openai-model value here.
func convertRequest(ar *AnthropicRequest, modelOverride string) *OpenAIRequest {
	req := &OpenAIRequest{
		Model:     ar.Model,
		MaxTokens: ar.MaxTokens,
		Stream:    ar.Stream,
	}
	if modelOverride != "" {
		req.Model = modelOverride
	}
	if ar.Temperature != nil {
		req.Temperature = ar.Temperature
	}
	if ar.TopP != nil {
		req.TopP = ar.TopP
	}
	if len(ar.Stop) > 0 {
		req.Stop = ar.Stop
	}

	// Multi-modal content uses an OpenAI content array; the first message
	// accumulates system text. We build messages sequentially.
	var msgs []OpenAIMessage

	// 1. system -> leading system-role message.
	sysText := systemToText(ar.System)
	// 2. conversation messages. Each Anthropic message may expand into
	// multiple OpenAI messages (a user message with tool_result blocks
	// becomes a user message plus one role=tool message per result).
	for _, m := range ar.Messages {
		msgs = append(msgs, splitMessage(m)...)
		// A user tool_result referencing a tool_use from the previous
		// assistant turn must immediately follow that assistant message.
		// Anthropic interleaves tool_use/tool_result across adjacent turns,
		// which maps 1:1 to OpenAI's assistant->tool adjacency. No reorder
		// is needed because Anthropic already orders them so.
	}

	// Prepend a system message if we have system text.
	sysMsgs := []OpenAIMessage{}
	if sysText != "" {
		sysMsgs = append(sysMsgs, OpenAIMessage{Role: "system", Content: sysText})
	}
	req.Messages = append(sysMsgs, msgs...)

	// 3. tools.
	if len(ar.Tools) > 0 {
		for _, t := range ar.Tools {
			req.Tools = append(req.Tools, convertTool(t))
		}
	}

	// 4. tool_choice.
	req.ToolChoice = convertToolChoice(ar.ToolChoice, ar.Tools)

	// 5. thinking -> optional mapping (ignored by default; upstream GLM
	//    reasons by default). We only map the "disabled" case to avoid
	//    forcing deep reasoning on non-thinking upstreams.
	if ar.Thinking != nil && ar.Thinking.Type == thinkmode.Disabled {
		// GLM accepts {"thinking":{"type":"disabled"}} on the OpenAI path.
		req.Thinking = map[string]any{"type": thinkmode.Disabled}
	}

	return req
}

// systemToText flattens the Anthropic system field (a plain string or a
// list of {type:text,text,cache_control} blocks) into a single string.
func systemToText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// string form
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	// array-of-blocks form
	var blocks []ContentBlock
	if json.Unmarshal(raw, &blocks) == nil {
		var out string
		for i, b := range blocks {
			if i > 0 {
				out += "\n"
			}
			out += b.Text
		}
		return out
	}
	return ""
}

// splitMessage turns one Anthropic message into zero or more OpenAI
// messages. A user message carrying tool_result blocks becomes a user
// message plus one role=tool message per result.
func splitMessage(m AnthropicMsg) []OpenAIMessage {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return []OpenAIMessage{{Role: m.Role, Content: s}}
	}
	var blocks []ContentBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return []OpenAIMessage{{Role: m.Role, Content: string(m.Content)}}
	}

	// Separate tool_result blocks (-> role=tool) from the rest.
	var toolResults []OpenAIMessage
	var rest []ContentBlock
	for _, b := range blocks {
		if b.Type == "tool_result" {
			toolResults = append(toolResults, OpenAIMessage{
				Role:       "tool",
				ToolCallID: b.ToolUseID,
				Content:    toolResultContent(b),
			})
			continue
		}
		rest = append(rest, b)
	}

	out := []OpenAIMessage{}
	if len(rest) > 0 {
		out = append(out, convertContentBlocks(m.Role, rest))
	}
	out = append(out, toolResults...)
	return out
}

func toolResultContent(b ContentBlock) string {
	var s string
	if json.Unmarshal(b.Content, &s) == nil {
		if b.IsError {
			return fmt.Sprintf("Error: %s", s)
		}
		return s
	}
	// content may be an array of {type:text,text} blocks.
	var parts []ContentBlock
	if json.Unmarshal(b.Content, &parts) == nil {
		var out string
		for _, p := range parts {
			out += p.Text
		}
		if b.IsError {
			return "Error: " + out
		}
		return out
	}
	return string(b.Content)
}

func convertContentBlocks(role string, blocks []ContentBlock) OpenAIMessage {
	switch role {
	case "assistant":
		var parts []string
		var calls []OpenAIToolCall
		for _, b := range blocks {
			switch b.Type {
			case "text":
				parts = append(parts, b.Text)
			case "thinking":
				// dropped
			case "tool_use":
				calls = append(calls, OpenAIToolCall{
					ID:   b.ID,
					Type: "function",
					Function: OpenAIFunctionCall{
						Name:      b.Name,
						Arguments: rawToJSONString(b.Input),
					},
				})
			}
		}
		om := OpenAIMessage{Role: "assistant", Content: joinText(parts)}
		if len(calls) > 0 {
			om.ToolCalls = calls
		}
		return om
	default:
		return OpenAIMessage{Role: role, Content: convertParts(blocks)}
	}
}

func joinText(parts []string) string {
	var out string
	for _, p := range parts {
		if out != "" {
			out += "\n"
		}
		out += p
	}
	return out
}

func convertParts(blocks []ContentBlock) any {
	// If any image present -> array; else joined text.
	hasImage := false
	for _, b := range blocks {
		if b.Type == "image" {
			hasImage = true
			break
		}
	}
	if !hasImage {
		var parts []string
		for _, b := range blocks {
			if b.Type == "text" {
				parts = append(parts, b.Text)
			}
		}
		return joinText(parts)
	}
	var parts []OpenAIContentPart
	for _, b := range blocks {
		switch b.Type {
		case "text":
			parts = append(parts, OpenAIContentPart{Type: "text", Text: b.Text})
		case "image":
			parts = append(parts, OpenAIContentPart{Type: "image_url", ImageURL: parseImageSource(b.Source)})
		}
	}
	return parts
}

func parseImageSource(src json.RawMessage) *OpenAIImageURL {
	var img struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
		URL       string `json:"url"`
	}
	if json.Unmarshal(src, &img) != nil {
		return &OpenAIImageURL{URL: ""}
	}
	if img.URL != "" {
		return &OpenAIImageURL{URL: img.URL}
	}
	// base64 data -> data URL.
	if img.Data != "" {
		return &OpenAIImageURL{URL: "data:" + img.MediaType + ";base64," + img.Data}
	}
	return &OpenAIImageURL{URL: ""}
}

func rawToJSONString(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	return string(raw)
}

func convertTool(t AnthropicTool) OpenAITool {
	params := t.InputSchema
	if len(params) == 0 {
		params = json.RawMessage(`{"type":"object","properties":{}}`)
	}
	// Anthropic input_schema is already an OpenAI-compatible JSON Schema.
	return OpenAITool{
		Type: "function",
		Function: json.RawMessage(fmt.Sprintf(
			`{"name":%s,"description":%s,"parameters":%s}`,
			jsonString(t.Name), jsonString(t.Description), string(params))),
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	return string(b)
}

// convertToolChoice maps Anthropic tool_choice to OpenAI. Returns any which
// is OK for the OpenAI endpoint; a specific tool maps to a function choice.
func convertToolChoice(raw json.RawMessage, tools []AnthropicTool) any {
	if len(raw) == 0 {
		return nil
	}
	var tc struct {
		Type string `json:"type"`
		Name string `json:"name"`
	}
	if json.Unmarshal(raw, &tc) != nil {
		return nil
	}
	switch tc.Type {
	case "auto", "any":
		return "auto"
	case "none":
		return "none"
	case "tool":
		return map[string]any{
			"type":     "function",
			"function": map[string]string{"name": tc.Name},
		}
	default:
		return "auto"
	}
}
