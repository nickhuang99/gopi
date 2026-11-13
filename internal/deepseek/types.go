// Package deepseek provides a Go client for the DeepSeek API via its
// Anthropic-compatible Claude Messages endpoint.
package deepseek

import (
	"encoding/json"
	"fmt"
)

// Message represents a single message in a chat conversation.
type Message struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
	// ThinkingSignature is the Claude API thinking-block signature that must
	// be replayed alongside ReasoningContent in assistant history. The
	// DeepSeek Anthropic-compatible endpoint rejects assistant messages that
	// drop thinking blocks. Empty on the OpenAI-style path.
	ThinkingSignature string     `json:"thinking_signature,omitempty"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID        string     `json:"tool_call_id,omitempty"`
	// Images carries the image blocks attached to ONE user message (native
	// multimodal input: the vision model sees the pixels directly — image
	// tokens are processed like text tokens, no OCR round-trip). When set,
	// Content is the accompanying instruction/prompt. A turn's N images ride
	// in a single message as N blocks: the endpoint accepts 8/9/12 images in
	// one user message (measured 2026-11-07, all 200), so splitting them
	// across consecutive user messages was the code's own invention, not the
	// API's requirement. Serialized so resume checkpoints keep the images.
	Images []ClaudeImageSource `json:"images,omitempty"`
}

// ToolCall represents a model-generated tool/function call.
// On the wire it is nested as {id, type:"function", function:{name, arguments}},
// but this struct exposes a flat Go representation with custom JSON handling.
//
// In streaming mode, tool calls arrive as fragments identified by Index.
// The caller must merge fragments with the same Index to reconstruct the full call.
// RawArgs holds the raw function.arguments string; in streaming mode arguments
// may arrive as incremental JSON fragments that are not individually parseable,
// so the caller should accumulate RawArgs across chunks and parse the combined
// string at the end.
type ToolCall struct {
	Index     int                    `json:"index,omitempty"`
	ID        string                 `json:"-"`
	Name      string                 `json:"-"`
	Arguments map[string]interface{} `json:"-"`
	RawArgs   string                 `json:"-"` // raw function.arguments string for streaming accumulation
}

// wireToolCall mirrors the DeepSeek/OpenAI wire format for a tool call.
type wireToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Index    *int             `json:"index,omitempty"`
	Function wireFunctionCall `json:"function"`
}

type wireFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string on the wire
}

// MarshalJSON encodes ToolCall into the nested wire format.
func (t ToolCall) MarshalJSON() ([]byte, error) {
	argsJSON, err := json.Marshal(t.Arguments)
	if err != nil {
		return nil, fmt.Errorf("deepseek: failed to marshal tool call arguments: %w", err)
	}
	w := wireToolCall{
		ID:   t.ID,
		Type: "function",
		Function: wireFunctionCall{
			Name:      t.Name,
			Arguments: string(argsJSON),
		},
	}
	return json.Marshal(w)
}

// UnmarshalJSON decodes ToolCall from the nested wire format.
// In streaming mode, function.arguments may be an incremental JSON fragment
// that is not valid JSON on its own. Rather than failing, we store the raw
// string in RawArgs so the caller can accumulate fragments across chunks
// and parse the combined string at the end.
func (t *ToolCall) UnmarshalJSON(data []byte) error {
	var w wireToolCall
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	t.ID = w.ID
	t.Name = w.Function.Name
	if w.Index != nil {
		t.Index = *w.Index
	}
	t.RawArgs = w.Function.Arguments
	if w.Function.Arguments != "" {
		if err := json.Unmarshal([]byte(w.Function.Arguments), &t.Arguments); err != nil {
			// Arguments may be an incremental JSON fragment in streaming mode.
			// Don't fail — store the raw string and let the caller accumulate.
			t.Arguments = make(map[string]interface{})
		}
	}
	if t.Arguments == nil {
		t.Arguments = make(map[string]interface{})
	}
	return nil
}

// Thinking controls the thinking mode for DeepSeek V4 models.
type Thinking struct {
	Type string `json:"type"` // thinkmode.Enabled or thinkmode.Disabled
	// BudgetTokens caps the per-round thinking budget. 0 = use the engine
	// default (defaultThinkingBudget). Kept far below max_tokens so thinking
	// cannot starve the actual reply/tool call — and cannot burn minutes per
	// round on reasoning chains (DeepSeek generates thinking at ~65 tok/s).
	BudgetTokens int `json:"budget_tokens,omitempty"`
}

// Usage contains token usage statistics including cache hit/miss.
type Usage struct {
	PromptTokens          int `json:"prompt_tokens"`
	CompletionTokens      int `json:"completion_tokens"`
	TotalTokens           int `json:"total_tokens"`
	PromptCacheHitTokens  int `json:"prompt_cache_hit_tokens,omitempty"`
	PromptCacheMissTokens int `json:"prompt_cache_miss_tokens,omitempty"`
}

// CostTracker accumulates API usage across an agent run.
type CostTracker struct {
	TotalPromptTokens     int
	TotalCompletionTokens int
	TotalCacheHitTokens   int
	TotalCacheMissTokens  int
}

// AddUsage accumulates a single Usage record.
func (ct *CostTracker) AddUsage(u Usage) {
	ct.TotalPromptTokens += u.PromptTokens
	ct.TotalCompletionTokens += u.CompletionTokens
	ct.TotalCacheHitTokens += u.PromptCacheHitTokens
	ct.TotalCacheMissTokens += u.PromptCacheMissTokens
}

// Summary returns a human-readable token usage summary.
func (ct *CostTracker) Summary() string {
	total := ct.TotalPromptTokens
	hit := ct.TotalCacheHitTokens
	miss := ct.TotalCacheMissTokens
	if miss == 0 && hit > 0 {
		miss = total - hit // approximate if cache_miss field not populated
	}
	return fmt.Sprintf(
		"prompt=%d(hit=%d miss=%d) completion=%d",
		total, hit, miss, ct.TotalCompletionTokens,
	)
}
