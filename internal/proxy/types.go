// Package proxy implements gopi's `gopi tool proxy` subcommand: a local HTTP
// server that accepts the Anthropic Messages protocol (/v1/messages + SSE)
// and translates it to the OpenAI Chat Completions protocol before forwarding
// to an OpenAI-compatible upstream (e.g. GLM-5.3-Flash). Responses are
// translated back to the Anthropic SSE wire format so unmodified Anthropic
// clients can talk to OpenAI-only providers.
//
// Zero external dependencies: net/http + encoding/json only.
package proxy

import "encoding/json"

// ---- Anthropic request wire types (inbound from the client) ----

// AnthropicRequest is the body of a POST /v1/messages request. Unknown
// fields are preserved via RawMessage passthrough where needed (image blocks).
type AnthropicRequest struct {
	Model       string          `json:"model"`
	MaxTokens   int             `json:"max_tokens"`
	Stream      bool            `json:"stream"`
	System      json.RawMessage `json:"system,omitempty"` // string | []SystemBlock
	Messages    []AnthropicMsg  `json:"messages"`
	Tools       []AnthropicTool `json:"tools,omitempty"`
	ToolChoice  json.RawMessage `json:"tool_choice,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
	TopP        *float64        `json:"top_p,omitempty"`
	TopK        *int            `json:"top_k,omitempty"`
	Stop        []string        `json:"stop_sequences,omitempty"`
	Thinking    *ThinkingConfig `json:"thinking,omitempty"`
	Metadata    json.RawMessage `json:"metadata,omitempty"`
}

// ThinkingConfig is the Anthropic extended-thinking block.
type ThinkingConfig struct {
	Type         string `json:"type"` // "enabled" | "disabled"
	BudgetTokens *int   `json:"budget_tokens,omitempty"`
}

// AnthropicMsg is one entry of the Anthropic messages array.
type AnthropicMsg struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"` // string | []ContentBlock
	Name    string          `json:"name,omitempty"`
	Extras  map[string]any  `json:"-"` // unknown top-level keys
}

// ContentBlock is one element of a content array. Type discriminates:
//   - text      -> Text
//   - image     -> Image
//   - tool_use  -> ToolUse
//   - tool_result -> ToolResult
//   - thinking  -> Thinking
type ContentBlock struct {
	Type string `json:"type"`

	// text / thinking
	Text      string `json:"text,omitempty"`
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`

	// image
	Source json.RawMessage `json:"source,omitempty"`

	// tool_use
	ID    string          `json:"id,omitempty"`
	Name  string          `json:"name,omitempty"`
	Input json.RawMessage `json:"input,omitempty"`

	// tool_result
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   json.RawMessage `json:"content,omitempty"`
	IsError   bool            `json:"is_error,omitempty"`
}

// AnthropicTool is a function declaration.
type AnthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// ---- OpenAI wire types (outbound to upstream / inbound from upstream) ----

// OpenAIRequest is the body sent to the OpenAI-compatible upstream.
type OpenAIRequest struct {
	Model           string          `json:"model"`
	Messages        []OpenAIMessage `json:"messages"`
	Tools           []OpenAITool    `json:"tools,omitempty"`
	ToolChoice      any             `json:"tool_choice,omitempty"`
	MaxTokens       int             `json:"max_tokens,omitempty"`
	Temperature     *float64        `json:"temperature,omitempty"`
	TopP            *float64        `json:"top_p,omitempty"`
	Stream          bool            `json:"stream,omitempty"`
	Stop            []string        `json:"stop,omitempty"`
	Thinking        map[string]any  `json:"thinking,omitempty"`
	ReasoningEffort *string         `json:"reasoning_effort,omitempty"`
}

// OpenAIMessage is one entry of the OpenAI messages array.
type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    any              `json:"content"` // string | []OpenAIContentPart
	Name       string           `json:"name,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	ToolCalls  []OpenAIToolCall `json:"tool_calls,omitempty"`
}

// OpenAIContentPart is one element of an OpenAI content array.
type OpenAIContentPart struct {
	Type     string          `json:"type"` // "text" | "image_url"
	Text     string          `json:"text,omitempty"`
	ImageURL *OpenAIImageURL `json:"image_url,omitempty"`
}

// OpenAIImageURL references an image by URL.
type OpenAIImageURL struct {
	URL string `json:"url"`
}

// OpenAITool is a function tool declaration.
type OpenAITool struct {
	Type     string          `json:"type"`     // "function"
	Function json.RawMessage `json:"function"` // {"name","description","parameters"}
}

// OpenAIToolCall is a function call in an assistant message.
type OpenAIToolCall struct {
	Index    int                `json:"index,omitempty"` // stream-delta tool index
	ID       string             `json:"id,omitempty"`
	Type     string             `json:"type,omitempty"` // "function"
	Function OpenAIFunctionCall `json:"function"`
}

// OpenAIFunctionCall carries the name and JSON-stringified arguments.
type OpenAIFunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"` // JSON-encoded string (may arrive incrementally)
}

// ---- OpenAI stream chunk (chat.completion.chunk) ----

// OpenAIChatChunk is one SSE data frame from the upstream stream.
type OpenAIChatChunk struct {
	ID      string              `json:"id,omitempty"`
	Object  string              `json:"object,omitempty"`
	Model   string              `json:"model,omitempty"`
	Choices []OpenAIChunkChoice `json:"choices"`
	Usage   *OpenAIUsage        `json:"usage,omitempty"`
}

// OpenAIChunkChoice is one choice in a chunk.
type OpenAIChunkChoice struct {
	Index        int         `json:"index"`
	Delta        OpenAIDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

// OpenAIDelta is the incremental change in a chunk.
type OpenAIDelta struct {
	Content          *string          `json:"content,omitempty"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	Role             string           `json:"role,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
}

// OpenAIPromptTokensDetails carries nested prompt-token breakdown fields
// (e.g. GLM's implicit prefix cache: prompt_tokens_details.cached_tokens).
type OpenAIPromptTokensDetails struct {
	CachedTokens int `json:"cached_tokens,omitempty"`
}

// OpenAIUsage is the token usage object.
type OpenAIUsage struct {
	PromptTokens        int                        `json:"prompt_tokens"`
	CompletionTokens    int                        `json:"completion_tokens"`
	TotalTokens         int                        `json:"total_tokens"`
	PromptTokensDetails *OpenAIPromptTokensDetails `json:"prompt_tokens_details,omitempty"`
}

// ---- OpenAI non-stream response ----

// OpenAIChatResponse is the non-stream completion response.
type OpenAIChatResponse struct {
	ID      string                 `json:"id"`
	Model   string                 `json:"model"`
	Choices []OpenAIResponseChoice `json:"choices"`
	Usage   *OpenAIUsage           `json:"usage,omitempty"`
}

// OpenAIResponseChoice wraps the full assistant message.
type OpenAIResponseChoice struct {
	Index        int                   `json:"index"`
	Message      OpenAIResponseMessage `json:"message"`
	FinishReason *string               `json:"finish_reason,omitempty"`
}

// OpenAIResponseMessage is the assistant message in a non-stream response.
type OpenAIResponseMessage struct {
	Role             string           `json:"role"`
	Content          *string          `json:"content,omitempty"`
	ReasoningContent *string          `json:"reasoning_content,omitempty"`
	ToolCalls        []OpenAIToolCall `json:"tool_calls,omitempty"`
}
