package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/nick/gopi/internal/rawlogfmt"
	"github.com/nick/gopi/internal/thinkmode"
)

// Claude Messages API (Anthropic-compatible) support.
//
// DeepSeek exposes a Claude-style endpoint at POST /anthropic/messages that
// was designed for agent-style tool use from day 1: tool calls arrive as
// native tool_use content blocks and the round ends with an explicit
// stop_reason ("end_turn" | "tool_use" | "max_tokens"). Unlike the OpenAI
// interface there is no finish_reason/tool_calls ambiguity.
//
// Wire format:
//
//	POST {base}/anthropic/messages
//	Authorization: Bearer <key>
//	anthropic-version: 2023-06-01
//	{
//	  "model": "deepseek-v4-flash",
//	  "max_tokens": 384000,
//	  "system": [{"type": "text", "text": "You are GoPi..."}],
//	  "messages": [
//	    {"role": "user", "content": [{"type": "text", "text": "..."}]},
//	    {"role": "assistant", "content": [
//	      {"type": "thinking", "thinking": "...", "signature": "..."},
//	      {"type": "text", "text": "I'll check..."},
//	      {"type": "tool_use", "id": "tool_1", "name": "read", "input": {...}}
//	    ]},
//	    {"role": "user", "content": [
//	      {"type": "tool_result", "tool_use_id": "tool_1", "content": "..."}
//	    ]}
//	  ]
//	}
//
// Two hard API rules discovered against the live endpoint:
//   - Thinking blocks MUST be replayed in assistant history (the API rejects
//     assistant messages that drop them); signatures arrive via
//     signature_delta events in streaming mode. Empty thinking blocks (no
//     thinking text streamed) must NOT be replayed — the API deserializer
//     requires the `thinking` field and rejects blocks that lost it (see
//     BuildClaudeRequest).
//   - Every tool_use block MUST be followed by a tool_result block in the
//     immediately next message — truncated tool calls must never enter history.

const (
	// claudeAPIPath is the Anthropic-compatible API prefix. The full request
	// path is {base}/anthropic/messages, except when the configured base URL
	// already ends with the prefix (e.g. ANTHROPIC_BASE_URL ends with
	// "/anthropic"), in which case only "/messages" is appended.
	claudeAPIPath = "/anthropic"
	// claudeMessagesPath is the full Claude Messages API request path.
	claudeMessagesPath = "/anthropic/messages"

	// claudeVersionHeader is the API-version header required by the
	// Anthropic-compatible endpoint.
	claudeVersionHeader = "anthropic-version"
	// claudeAPIVersion is the Anthropic API version implemented by DeepSeek.
	claudeAPIVersion = "2023-06-01"

	// DeepSeekV4MaxOutputTokens is DeepSeek V4's maximum output per response:
	// 384K tokens (with a 1M-token context window), per the DeepSeek Pi
	// integration docs (see devdoc/optimization-report.md). The Claude
	// Messages API requires max_tokens to be set explicitly, so this ceiling
	// is the natural default — the model generates only what it needs, and
	// the cap exists purely to satisfy the API contract.
	DeepSeekV4MaxOutputTokens = 384000

	// DefaultClaudeMaxTokens is the default max_tokens for Claude requests:
	// 16384 — the converged value from the 384K ceiling (A4, measured live
	// 2026-08-22 against deepseek-v4-flash; see devdoc/maxtokens-converged.md).
	//
	// The endpoint HARD-ENFORCES max_tokens (a 8192 cap stops the stream at
	// exactly 8192 output tokens with stop_reason=max_tokens). A 607-line
	// single write draft — the worst realistic case — measured 18,685 escaped
	// chars ≈ 5,968 completion tokens, and the worst thinking+draft round
	// totaled ~7,500 of 8,192 tokens (91.5% utilization) WITHOUT truncation.
	// 8,192 therefore fits the measured case with only ~8% headroom, and the
	// devdoc's documented thousand-line-draft scenario (~9.6K tokens) would
	// truncate at 8192 — re-entering the re-issue loop that truncation saga
	// fixed twice (4096 → 32768 → 384K, devdoc/maxtokens-truncation.md).
	// 16,384 keeps 2x margin over the measured worst case while bounding the
	// per-response ceiling 23x below the model's 384K limit. GOPI_MAX_TOKENS
	// overrides when a tighter (or larger) budget is wanted.
	DefaultClaudeMaxTokens = 16384

	// DefaultClaudeThinkingBudget is the default per-round thinking budget.
	// Deliberately modest: DeepSeek generates thinking at ~65 tok/s (measured
	// live), so a large budget costs real wall time — 16384 tokens of thinking
	// would take ~4 minutes per round. 1024 tokens of reasoning is ample for
	// agent rounds; GOPI_THINKING_BUDGET overrides it.
	//
	// 2048 → 1024 (gopi-race R5-R6 post-mortem, session 952b7b73): the API
	// does NOT honor budget_tokens — the harness aborts rounds whose streamed
	// thinking exceeds the budget and re-issues them thinking-disabled. Every
	// such round pays TWO full prefills, so the budget is also the abort
	// point: at 2048 (~32s of thinking at 65 tok/s) a design-heavy round that
	// keeps thinking is aborted 32s in; at 1024 the same round is aborted
	// ~16s in — half the wasted generation, same re-issue semantics. Rounds
	// with naturally light thinking (the measured sweet spot: 171-400 chars)
	// are untouched (1024 tokens = 4096 chars).
	DefaultClaudeThinkingBudget = 1024

	// EmptyThinkingSignature is the signature attached to harness-injected
	// EMPTY thinking blocks. The Anthropic-compatible endpoint requires every
	// assistant message that carries a tool_use to also carry a thinking block
	// when thinking mode is enabled (verified live 2026-08-21: dropping it
	// 400s with "The `content[].thinking` in the thinking mode must be passed
	// back to the API"; an EMPTY block satisfies the requirement). Rounds
	// generated with thinking disabled (harness budget downgrade) or rounds
	// whose thinking streamed empty (bug024) have no thinking text to replay —
	// the harness emits this placeholder instead. Any non-empty signature is
	// accepted; the constant keeps the wire deterministic.
	EmptyThinkingSignature = "gopi-empty-thinking"

	// DefaultVisionModel is the default multimodal model for image
	// understanding (vision_analyze). deepseek-v4-flash-vision-exp — the
	// vision variant of the flash family: same price as flash, ~384 tokens
	// per image after scaling (a 2000×2000 and a 5000×5000 image cost the
	// same). Overridable via settings.json "vision_model" / GOPI_VISION_MODEL.
	DefaultVisionModel = "deepseek-v4-flash-vision-exp"

	// DefaultVisionBaseURL is the default vision endpoint: the same
	// Anthropic-compatible base as the main model. Overridable via
	// settings.json "vision_base_url" / GOPI_VISION_BASE_URL — switching to
	// another multimodal provider is a settings change, zero code.
	DefaultVisionBaseURL = "https://api.deepseek.com/anthropic"

	// DefaultVisionMaxTokens bounds the vision analysis reply. Image
	// understanding / OCR replies are short (a description or recognized
	// text), so a tight cap keeps cost and latency low while leaving ample
	// room for any text the model chooses to emit.
	DefaultVisionMaxTokens = 2048
)

// isGLMModel reports whether the model is a Zhipu GLM model (e.g.
// glm-5.3-flash). GLM's Anthropic endpoint has one hard behavioural quirk:
// it ALWAYS thinks and REJECTS thinking:disabled with HTTP 400 error 1210
// ("该模型始终思考，不支持关闭思考；请使用 low、high 或 max"). Callers use this
// to omit the thinking field for GLM instead of sending disabled.
func isGLMModel(model string) bool {
	m := strings.ToLower(strings.TrimSpace(model))
	return strings.HasPrefix(m, "glm-") || strings.HasPrefix(m, "glm_") || strings.Contains(m, "/glm-")
}

// ClaudeCacheControl is the Anthropic prompt-caching marker
// ({"type": "ephemeral"}). The DeepSeek /anthropic endpoint ACCEPTS it and
// serves cache reads for the marked prefix across requests (verified live
// 2026-08-22: system+tools marked, cache_read=256 on a follow-up request with
// a different user message); cache_creation is never reported there — DeepSeek
// runs its own content-addressed auto-cache, so the marker is additive and
// harmless on this endpoint, and required for real Anthropic endpoints.
type ClaudeCacheControl struct {
	Type string `json:"type"`
}

// ClaudeImageSource is the source of an image content block
// ({"type": "image", "source": {"type": "base64", "media_type": "image/jpeg",
// "data": "<base64>"}}). base64 inline is the simplest path for local files
// (the vision_analyze tool); external URLs and the Files API are alternative
// transports DeepSeek also accepts. Only USER messages may carry images —
// system/assistant messages with images are rejected with HTTP 400.
//
// A source has TWO transports and the block type stays "image" for both:
//   - inline:      {"type":"base64","media_type":…,"data":…}
//   - files api:   {"type":"file","file_id":…}
//
// The Files API transport is NOT spelled {"type":"file","file_id":…} as a
// MESSAGE CONTENT BLOCK — that is refused with HTTP 422 "unknown variant
// `file`" (measured 2026-11-06, both flat and nested). The reference lives in
// the SOURCE; the block stays an image block. MediaType/Data are empty for a
// file source and FileID is empty for an inline one.
type ClaudeImageSource struct {
	Type      string `json:"type"`                 // "base64" | "file"
	MediaType string `json:"media_type,omitempty"` // image/jpeg | image/png | image/gif | image/webp
	Data      string `json:"data,omitempty"`       // base64-encoded bytes
	FileID    string `json:"file_id,omitempty"`    // Files API handle (Type "file")
}

// ClaudeImageBlock is an image content block inside a user message.
type ClaudeImageBlock struct {
	Type   string            `json:"type"` // "image"
	Source ClaudeImageSource `json:"source"`
}

// cacheControlEphemeral is the shared "cache this block" marker. A shared
// instance is safe: it is only ever serialized, never mutated.
var cacheControlEphemeral = ClaudeCacheControl{Type: "ephemeral"}

// ClaudeSystemBlock is a system prompt block (array form is always used).
// CacheControl marks the block for prompt caching (Anthropic standard).
type ClaudeSystemBlock struct {
	Type         string              `json:"type"`
	Text         string              `json:"text"`
	CacheControl *ClaudeCacheControl `json:"cache_control,omitempty"`
}

// ClaudeContentBlock is a content block inside a Claude message:
//   - user text:       {type: "text", text}
//   - assistant text:  {type: "text", text}
//   - assistant think: {type: "thinking", thinking, signature}
//   - tool call:       {type: "tool_use", id, name, input}
//   - tool result:     {type: "tool_result", tool_use_id, content}
//
// Input has NO omitempty: Go's omitempty drops empty maps, and DeepSeek's
// deserializer rejects tool_use blocks that lost their `input` field with
// HTTP 400 "missing field `input`" (verified live). BuildClaudeRequest
// guarantees Input is non-nil for every tool_use block; the other block
// types emit "input":null, which the API tolerates.
//
// InputJSON is a scratch buffer for streaming input_json_delta fragments and
// is never serialized.
type ClaudeContentBlock struct {
	Type      string                 `json:"type"`
	Text      string                 `json:"text,omitempty"`
	Thinking  string                 `json:"thinking,omitempty"`
	Signature string                 `json:"signature,omitempty"`
	ID        string                 `json:"id,omitempty"`
	Name      string                 `json:"name,omitempty"`
	Input     map[string]interface{} `json:"input"`
	ToolUseID string                 `json:"tool_use_id,omitempty"`
	Content   interface{}            `json:"content,omitempty"`
	Source    *ClaudeImageSource     `json:"source,omitempty"` // image blocks (vision)
	InputJSON string                 `json:"-"`                // accumulated input_json_delta fragments
}

// ClaudeMessage is a single message in the Claude Messages API format.
// Note: tool results use role "user" with tool_result content blocks.
type ClaudeMessage struct {
	Role    string               `json:"role"` // "user" | "assistant"
	Content []ClaudeContentBlock `json:"content"`
}

// UnmarshalJSON accepts both wire shapes that appear in raw-log checkpoints:
// the current array form ([]ClaudeContentBlock, produced by
// BuildClaudeRequest since the Claude-path switch) and the legacy string
// form ({"role":"user","content":"text"} — pre-2026-08 raw logs). A string
// content becomes a single text block, so old logs stay resumable. Only the
// checkpoint parser unmarshals ClaudeMessage (requests are marshaled by
// BuildClaudeRequest; responses use ClaudeStreamEvent), so this tolerance
// cannot mask bugs in the hot path.
func (m *ClaudeMessage) UnmarshalJSON(data []byte) error {
	var raw struct {
		Role    string          `json:"role"`
		Content json.RawMessage `json:"content"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	m.Role = raw.Role
	var blocks []ClaudeContentBlock
	if len(raw.Content) > 0 && raw.Content[0] == '"' {
		var s string
		if err := json.Unmarshal(raw.Content, &s); err != nil {
			return err
		}
		blocks = []ClaudeContentBlock{{Type: "text", Text: s}}
	} else if len(raw.Content) > 0 {
		if err := json.Unmarshal(raw.Content, &blocks); err != nil {
			return err
		}
	}
	m.Content = blocks
	return nil
}

// ClaudeToolDef defines a tool for the Claude API. The schema uses the
// Anthropic input_schema format (equivalent to OpenAI's parameters).
// CacheControl marks the tool definition for prompt caching (Anthropic
// convention: one marker on the LAST tool entry caches the whole tools array).
type ClaudeToolDef struct {
	Name         string              `json:"name"`
	Description  string              `json:"description,omitempty"`
	InputSchema  interface{}         `json:"input_schema"`
	CacheControl *ClaudeCacheControl `json:"cache_control,omitempty"`
}

// ClaudeThinking controls thinking mode (DeepSeek V4). Omitted when nil —
// DeepSeek's Anthropic-compatible endpoint enables thinking by default.
type ClaudeThinking struct {
	Type         string `json:"type"` // thinkmode.Enabled | thinkmode.Disabled
	BudgetTokens int    `json:"budget_tokens,omitempty"`
}

// ClaudeUsage mirrors the Anthropic usage counters. DeepSeek reports
// input/output/cache_read; cache_creation is usually 0 on this endpoint.
type ClaudeUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// TotalTokens returns the sum of all Claude usage counters.
func (u ClaudeUsage) TotalTokens() int {
	return u.InputTokens + u.OutputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// AddClaudeUsage accumulates Claude-format usage into the cost tracker.
// Prompt side = fresh input + cache writes + cache reads; completion side =
// output tokens.
func (ct *CostTracker) AddClaudeUsage(u ClaudeUsage) {
	ct.TotalPromptTokens += u.PromptSideTokens()
	ct.TotalCompletionTokens += u.OutputTokens
	ct.TotalCacheHitTokens += u.CacheReadInputTokens
	ct.TotalCacheMissTokens += u.CacheCreationInputTokens + u.InputTokens
}

// ClaudeRequest is the top-level request body for the Claude Messages API.
type ClaudeRequest struct {
	Model     string              `json:"model"`
	MaxTokens int                 `json:"max_tokens"`
	System    []ClaudeSystemBlock `json:"system,omitempty"`
	Messages  []ClaudeMessage     `json:"messages"`
	Tools     []ClaudeToolDef     `json:"tools,omitempty"`
	Stream    bool                `json:"stream,omitempty"`
	Thinking  *ClaudeThinking     `json:"thinking,omitempty"`
	// ReasoningEffort is DeepSeek's reasoning-depth enum (off | minimal |
	// low | medium | high | max), sent verbatim on the wire. It is only sent
	// while thinking is enabled; "off" is resolved client-side to thinking
	// disabled and never reaches the wire (dsh resolveThinking semantics).
	//
	// THE ENDPOINT ACCEPTS IT BUT DOES NOT HONOR IT — accept (HTTP 200 on every
	// value) is not control. Two independent measurements agree, and the probe
	// that produced the second one is printed below, because the claim this
	// comment replaces was wrong in the expensive direction: it named a date
	// and a 6x multiple, which is exactly the shape that stops the next reader
	// from re-testing it.
	//
	//	devdoc/thinking-budget.md §1 (2026-08-21, re-measured 2026-09-15):
	//	reasoning_effort=minimal streamed 29,016 thinking chars and =high
	//	27,092 — the "deeper" setting produced FEWER. The same table shows
	//	budget_tokens 128 / 1024 / 2048 all running away
	//	(6,148 / 6,526 chars, ~15k tok) under `max_tokens`. Neither knob is a
	//	cap; only thinking:disabled is honored (0 thinking chars, full text).
	//
	//	2026-11-14, direct probe — the exact command is printed below.
	//	Streamed thinking chars, summed over content_block_delta.delta.thinking.
	//	Three runs of the SAME prompt, only budget_tokens differing:
	//
	//	  run      budget_tokens=128    budget_tokens=8192
	//	  1        1,199                1,034
	//	  2        984                  1,151
	//	  3        708                  930
	//
	//	Read the 128 COLUMN, not the pair: a 128-token thinking budget cannot
	//	produce 708–1,199 chars (~177–300 tokens at 4 chars/token) — it is
	//	exceeded by up to 2x on the very run it was set on. That is a cap not
	//	being enforced, and it does not depend on which column comes out
	//	larger, because the ORDER FLIPS between runs (1199>1034, then
	//	984<1151, then 708<930) — so a single pair is not the evidence; the
	//	exceeded budget is, plus a spread that overlaps across a 64x range.
	//
	//	Arm A asks whether `reasoning_effort` moves depth: minimal → 11,945,
	//	max → 11,590 on one prompt (3% apart, MAX SMALLER); 780 vs 908 on
	//	another. Both directions → sampling noise, not a control.
	//
	// So this field is a DECLARATION, not a control — the value a caller sets
	// here does not bound thinking on this endpoint, and the harness's own
	// abort threshold (EffectiveThinkingBudget × thinkingCharsPerToken) is the
	// only thing that does; see devdoc/thinking-budget.md §1 for why that
	// threshold must be recoverable.
	//
	// REPRODUCE IT (needs curl + python3 and the api_key already in
	// ~/.gopi/settings.json; nothing about gopi runs). Paste as-is:
	//
	//	KEY=$(python3 -c 'import json,os;print(json.load(open(os.path.expanduser("~/.gopi/settings.json")))["api_key"])')
	//	for B in 128 8192; do
	//	  J="{\"model\":\"deepseek-v4-flash\",\"max_tokens\":4096,\"stream\":true,\"thinking\":{\"type\":\"enabled\",\"budget_tokens\":$B},\"messages\":[{\"role\":\"user\",\"content\":\"Prove that the sum of the first n odd integers is n squared, rigorously.\"}]}"
	//	  echo -n "budget_tokens=$B thinking_chars="
	//	  curl -sS https://api.deepseek.com/anthropic/v1/messages -H "Authorization: Bearer $KEY" -H 'content-type: application/json' -H 'anthropic-version: 2023-06-01' --data-binary "$J" | python3 -c 'import sys,json;print(sum(len((json.loads(l[5:]).get("delta") or {}).get("thinking") or "") for l in sys.stdin if l.startswith("data:") and l[5:].strip() not in ("","[DONE]")))'
	//	done
	//
	// A HONORING endpoint keeps BOTH rows under ~512 chars (128 tok × 4) and
	// makes the 8192 row the larger by a wide margin. Measured, 128 overshoots
	// its own cap every run and the two rows overlap. For the
	// `reasoning_effort` half, add `\"reasoning_effort\":\"minimal\"` (then
	// `\"max\"`) to J and diff the totals: they land within a few percent, in
	// whichever direction the sampling goes.
	ReasoningEffort string `json:"reasoning_effort,omitempty"`
}

// ClaudeStreamDelta is the delta payload of a content_block_delta event.
type ClaudeStreamDelta struct {
	Type        string // "text_delta" | "thinking_delta" | "input_json_delta" | "signature_delta"
	Text        string
	Thinking    string
	PartialJSON string
	Signature   string
}

// ClaudeStreamEvent is one parsed SSE event from the Claude Messages API.
// Type is one of: message_start | content_block_start | content_block_delta
// | content_block_stop | message_delta | message_stop | ping | error.
// Fields are populated only for the event types that carry them.
type ClaudeStreamEvent struct {
	Type       string
	Index      int
	Block      *ClaudeContentBlock
	Delta      *ClaudeStreamDelta
	StopReason string // message_delta: end_turn | tool_use | max_tokens | stop_sequence
	MessageID  string // message_start
	Usage      ClaudeUsage
	Error      string // transport error or API error event
}

// ClaudeResponse is an accumulated Claude Messages API response (streaming
// or non-streaming). Content blocks are in wire order.
type ClaudeResponse struct {
	ID         string
	Content    []ClaudeContentBlock
	StopReason string // end_turn | tool_use | max_tokens | stop_sequence
	Usage      ClaudeUsage
}

// Text returns the concatenation of all text blocks in wire order.
func (r *ClaudeResponse) Text() string {
	var sb strings.Builder
	for _, b := range r.Content {
		if b.Type == "text" {
			sb.WriteString(b.Text)
		}
	}
	return sb.String()
}

// Thinking returns the concatenated thinking text and the last non-empty
// signature. Thinking blocks must be replayed in assistant history, so the
// agent stores both.
func (r *ClaudeResponse) Thinking() (thinking string, signature string) {
	var sb strings.Builder
	for _, b := range r.Content {
		if b.Type != "thinking" {
			continue
		}
		sb.WriteString(b.Thinking)
		if b.Signature != "" {
			signature = b.Signature
		}
	}
	return sb.String(), signature
}

// ToolCalls returns the tool_use blocks mapped to GoPi ToolCall values.
func (r *ClaudeResponse) ToolCalls() []ToolCall {
	var calls []ToolCall
	for _, b := range r.Content {
		if b.Type != "tool_use" {
			continue
		}
		calls = append(calls, ToolCall{
			ID:        b.ID,
			Name:      b.Name,
			Arguments: b.Input,
		})
	}
	return calls
}

// MapClaudeStopReason maps a Claude stop_reason to the GoPi finishReason
// vocabulary the agent loop already understands:
//
//	end_turn       → "stop"        (model finished the round)
//	tool_use       → "tool_calls"  (model requested tools — must execute)
//	max_tokens     → "length"      (output truncated — re-issue)
//	stop_sequence  → "stop"        (no stop sequences are configured)
//	refusal        → "content_filter" (response blocked)
func MapClaudeStopReason(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "refusal":
		return "content_filter"
	default: // end_turn, stop_sequence, pause_turn, "" (cut stream)
		return "stop"
	}
}

// EffectiveThinkingBudget computes the per-request thinking budget: the
// configured budget (GOPI_THINKING_BUDGET) or DefaultClaudeThinkingBudget when
// unset, capped at half the output cap (the API requires budget_tokens <
// max_tokens, and thinking must never starve the reply).
//
// Exported because the cap is a Rule 15 hazard: a configured budget above
// max_tokens/2 is silently lowered, and the agent loop must be able to REPORT
// the clamp (configured 16384 with the default max_tokens=16384 has an
// effective budget of 8192 — which is also the harness's abort point). The
// agent compares this against its configured value and logs the difference
// once per run.
//
// Thinking gets at most half the output budget so it can never starve the
// actual reply / tool call — with a fixed budget the model once spent its
// ENTIRE output allowance on thinking and never emitted the write tool call,
// truncating a file draft with stop_reason=max_tokens. Scaling with max_tokens
// keeps the ratio stable when the cap is raised, and the result is always
// strictly below max_tokens as the API requires.
func EffectiveThinkingBudget(maxTokens, configured int) int {
	budget := DefaultClaudeThinkingBudget
	if configured > 0 {
		budget = configured
	}
	if half := maxTokens / 2; budget > half {
		return half
	}
	return budget
}

// BuildClaudeRequest converts GoPi's internal message history (OpenAI-style
// roles: system/user/assistant/tool) into a Claude Messages API request.
//
// Conversion rules (enforced by the live API):
//   - System messages become top-level system text blocks (systemPrompt,
//     then projectContext).
//   - Assistant messages replay thinking blocks with signatures — the API
//     rejects history that drops them.
//   - Consecutive tool results are coalesced into a single user message with
//     multiple tool_result blocks.
//   - Consecutive user text messages are merged (roles must alternate).
//   - Empty messages are dropped.
func BuildClaudeRequest(model string, msgs []Message, systemPrompt, projectContext string, tools []ClaudeToolDef, maxTokens int, thinking *Thinking, reasoningEffort ...string) *ClaudeRequest {
	req := &ClaudeRequest{
		Model:     model,
		MaxTokens: maxTokens,
	}
	if systemPrompt != "" {
		req.System = append(req.System, ClaudeSystemBlock{Type: "text", Text: systemPrompt})
	}
	if projectContext != "" {
		req.System = append(req.System, ClaudeSystemBlock{Type: "text", Text: projectContext})
	}
	// Prompt caching (Anthropic standard, A5): mark the LAST system block and
	// the last tool def. The tools slice is copied first so the marker never
	// mutates the caller's definitions (claudeToolDefs is package-global).
	if n := len(req.System); n > 0 {
		req.System[n-1].CacheControl = &cacheControlEphemeral
	}
	if n := len(tools); n > 0 {
		req.Tools = make([]ClaudeToolDef, n)
		copy(req.Tools, tools)
		req.Tools[n-1].CacheControl = &cacheControlEphemeral
	}
	if thinking != nil {
		req.Thinking = &ClaudeThinking{Type: thinking.Type}
		if thinking.Type != thinkmode.Disabled {
			req.Thinking.BudgetTokens = EffectiveThinkingBudget(maxTokens, thinking.BudgetTokens)
		}
		// GLM reject thinking:disabled outright (verified live 2026-08-27:
		// HTTP 400 error 1210 "该模型始终思考，不支持关闭思考；请使用 low、high
		// 或 max" — this model ALWAYS thinks). GLM has no off switch, so when
		// a GLM model is asked for thinking-disabled we simply omit the
		// thinking field: GLM then thinks natively and ignores any budget. The
		// harness already streams and replays thinking blocks + signatures, so
		// always-on thinking is transparent. DeepSeek models are unaffected
		// (they honor thinking:disabled, so the field stays).
		if isGLMModel(model) && req.Thinking.Type == thinkmode.Disabled {
			req.Thinking = nil
		}
	}
	// Reasoning effort (P0-1): wired only when thinking is enabled and the
	// value is a real effort (not "" / "off" — off is resolved to thinking
	// disabled by the caller, never sent). Variadic so existing callers
	// (compact context) stay unchanged.
	if len(reasoningEffort) > 0 && req.Thinking != nil && req.Thinking.Type != thinkmode.Disabled {
		if e := reasoningEffort[0]; e != "" && e != "off" {
			req.ReasoningEffort = e
		}
	}

	// pendingToolResults accumulates consecutive tool-result messages so they
	// can be coalesced into one user message with multiple tool_result blocks.
	var pendingToolResults []ClaudeContentBlock
	flushToolResults := func() {
		if len(pendingToolResults) == 0 {
			return
		}
		req.Messages = append(req.Messages, ClaudeMessage{Role: "user", Content: pendingToolResults})
		pendingToolResults = nil
	}

	for _, m := range msgs {
		switch m.Role {
		case "system":
			// System prompts are promoted to the top-level system blocks above.
			continue

		case "tool":
			pendingToolResults = append(pendingToolResults, ClaudeContentBlock{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			})

		case "user":
			flushToolResults()
			if strings.TrimSpace(m.Content) == "" && len(m.Images) == 0 {
				continue
			}
			// One image block per image, then the text instruction. Images are
			// only valid in USER messages. Image tokens are processed natively
			// by the vision model (no OCR round-trip).
			var userBlocks []ClaudeContentBlock
			for i := range m.Images {
				userBlocks = append(userBlocks, ClaudeContentBlock{
					Type:   claudeImageBlockType,
					Source: &m.Images[i],
				})
			}
			if strings.TrimSpace(m.Content) != "" {
				userBlocks = append(userBlocks, ClaudeContentBlock{Type: "text", Text: m.Content})
			}
			// Merge ALL consecutive user messages, images included: the API
			// requires alternating roles and accepts extra blocks in a
			// tool-result user message. Measured 2026-11-07: one user message
			// carrying 8/9/12 image blocks is accepted (HTTP 200), so the
			// earlier "each image must travel in its own message" rule was
			// this code's own invention — it split one turn into N user
			// messages the wire then had to keep apart.
			if n := len(req.Messages); n > 0 && req.Messages[n-1].Role == "user" {
				req.Messages[n-1].Content = append(req.Messages[n-1].Content, userBlocks...)
				continue
			}
			req.Messages = append(req.Messages, ClaudeMessage{
				Role:    "user",
				Content: userBlocks,
			})

		case "assistant":
			flushToolResults()
			var blocks []ClaudeContentBlock
			// Thinking blocks are replayed only when they carry actual
			// thinking text; the API rejects assistant history that drops
			// non-empty thinking blocks. DeepSeek sometimes streams an EMPTY
			// thinking block (signature may be a bogus message id) — such a
			// block MUST NOT be replayed: Go's omitempty drops the empty
			// `thinking` value, and DeepSeek's deserializer rejects the
			// resulting {"type":"thinking"} block with HTTP 400 "missing
			// field `thinking`" (verified live). An empty signature on a
			// non-empty thinking block (interrupted stream) is accepted.
			if m.ReasoningContent != "" {
				blocks = append(blocks, ClaudeContentBlock{
					Type:      "thinking",
					Thinking:  m.ReasoningContent,
					Signature: m.ThinkingSignature,
				})
			}
			// Thinking-mode wire rule (verified live 2026-08-21): when the
			// current request has thinking enabled, every assistant message
			// containing a tool_use MUST carry a thinking block — the API
			// rejects tool rounds whose thinking was dropped with HTTP 400
			// "The `content[].thinking` in the thinking mode must be passed
			// back to the API". This arises when the tool round was generated
			// with thinking disabled (harness budget downgrade) or streamed an
			// EMPTY thinking block (bug024 — dropped by the empty-replay rule
			// above). An empty block satisfies the requirement (verified
			// live); the field must still be PRESENT on the wire, so the
			// placeholder carries a single space (omitempty would drop a true
			// empty string and the deserializer rejects the block).
			if len(m.ToolCalls) > 0 && m.ReasoningContent == "" && req.Thinking != nil && req.Thinking.Type == thinkmode.Enabled {
				blocks = append(blocks, ClaudeContentBlock{
					Type:      "thinking",
					Thinking:  " ",
					Signature: EmptyThinkingSignature,
				})
			}
			if m.Content != "" {
				blocks = append(blocks, ClaudeContentBlock{Type: "text", Text: m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := tc.Arguments
				if input == nil {
					// The API requires the `input` field on tool_use blocks;
					// a nil/empty map would be dropped by omitempty → 400
					// "missing field `input`" (verified live).
					input = map[string]interface{}{}
				}
				blocks = append(blocks, ClaudeContentBlock{
					Type:  "tool_use",
					ID:    tc.ID,
					Name:  tc.Name,
					Input: input,
				})
			}
			if len(blocks) == 0 {
				continue
			}
			req.Messages = append(req.Messages, ClaudeMessage{Role: "assistant", Content: blocks})
		}
	}
	flushToolResults()
	return req
}

// ResolveClaudeAPIKey returns the API key for the Claude-compatible endpoint.
// Resolution: ANTHROPIC_AUTH_TOKEN env > DEEPSEEK_API_KEY env > settings
// api_key > error. The Anthropic-compatible endpoint uses the same DeepSeek
// key, so DEEPSEEK_API_KEY is a valid fallback.
func ResolveClaudeAPIKey(cfg *Config) (string, error) {
	if k := os.Getenv("ANTHROPIC_AUTH_TOKEN"); k != "" {
		return k, nil
	}
	if k := os.Getenv("DEEPSEEK_API_KEY"); k != "" {
		return k, nil
	}
	if cfg != nil && cfg.APIKey != "" {
		return cfg.APIKey, nil
	}
	return "", fmt.Errorf("deepseek: ANTHROPIC_AUTH_TOKEN not set (env, DEEPSEEK_API_KEY, or ~/.gopi/settings.json)")
}

// ResolveClaudeBaseURL returns the base URL for the Claude-compatible
// endpoint. Resolution: ANTHROPIC_BASE_URL env > DEEPSEEK_BASE_URL env >
// settings base_url > default. ANTHROPIC_BASE_URL commonly includes the
// "/anthropic" prefix (e.g. https://api.deepseek.com/anthropic); the client
// appends "/messages" accordingly.
func ResolveClaudeBaseURL(cfg *Config) string {
	if u := os.Getenv("ANTHROPIC_BASE_URL"); u != "" {
		return u
	}
	if u := os.Getenv("DEEPSEEK_BASE_URL"); u != "" {
		return u
	}
	if cfg != nil && cfg.BaseURL != "" {
		return cfg.BaseURL
	}
	return "https://api.deepseek.com"
}

// NewClaudeClient creates a Client configured for the Claude Messages API.
// API key and base URL are resolved via ANTHROPIC_* env vars first, falling
// back to the existing DEEPSEEK_* / settings.json values.
func NewClaudeClient() (*Client, error) {
	cfg, err := LoadConfig(SettingsPath())
	if err != nil {
		return nil, fmt.Errorf("deepseek: failed to load config: %w", err)
	}

	apiKey, err := ResolveClaudeAPIKey(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		BaseURL:     ResolveClaudeBaseURL(cfg),
		APIKey:      apiKey,
		HTTPClient:  newHTTPClient(),
		RetryConfig: DefaultRetryConfig(),
		// MessagesPath is a settings-only override; no env var. Empty keeps
		// the legacy {base}/anthropic/messages vs {base}/v1/messages logic.
		MessagesPath: claudeMessagesPathFromConfig(cfg),
	}, nil
}

// claudeMessagesPathFromConfig returns the settings messages_path override,
// or "" when unset (nil config safe).
func claudeMessagesPathFromConfig(cfg *Config) string {
	if cfg != nil {
		return cfg.MessagesPath
	}
	return ""
}

// ResolveVisionBaseURL returns the base URL for the vision endpoint.
// Resolution: GOPI_VISION_BASE_URL env > settings vision_base_url >
// DefaultVisionBaseURL (the DeepSeek Anthropic-compatible endpoint). The
// client appends "/v1/messages" to it exactly like claudeMessagesURL does, so
// the same with/without "/anthropic" suffix tolerance applies.
func ResolveVisionBaseURL(cfg *Config) string {
	if u := os.Getenv("GOPI_VISION_BASE_URL"); u != "" {
		return u
	}
	if cfg != nil && cfg.VisionBaseURL != "" {
		return cfg.VisionBaseURL
	}
	return DefaultVisionBaseURL
}

// ResolveVisionModel returns the vision model name.
// Resolution: GOPI_VISION_MODEL env > settings vision_model >
// DefaultVisionModel.
func ResolveVisionModel(cfg *Config) string {
	if m := os.Getenv("GOPI_VISION_MODEL"); m != "" {
		return m
	}
	if cfg != nil && cfg.VisionModel != "" {
		return cfg.VisionModel
	}
	return DefaultVisionModel
}

// NewVisionClient creates a Client configured for the vision endpoint. API
// key is resolved exactly like the main Claude client (ANTHROPIC_AUTH_TOKEN >
// DEEPSEEK_API_KEY > settings api_key — same DeepSeek key); base URL uses the
// vision-specific resolution so a future multimodal provider can be swapped
// in via settings.json with zero code changes.
func NewVisionClient() (*Client, error) {
	cfg, err := LoadConfig(SettingsPath())
	if err != nil {
		return nil, fmt.Errorf("deepseek: failed to load config: %w", err)
	}

	apiKey, err := ResolveClaudeAPIKey(cfg)
	if err != nil {
		return nil, err
	}

	return &Client{
		BaseURL:     ResolveVisionBaseURL(cfg),
		APIKey:      apiKey,
		HTTPClient:  newHTTPClient(),
		RetryConfig: DefaultRetryConfig(),
	}, nil
}

// VisionAnalyze sends one image (base64 inline) plus an optional prompt to
// the vision model and returns the model's understanding (description / OCR
// text). The image travels ONLY in this single request — the result handed
// back to the agent is plain text, so the image bytes never enter the main
// context (context protection). Images are only valid in USER messages, which
// is exactly what this builds. The returned usage lets the caller report the
// per-image token cost (expected ≤ 384 per image — the vision model's fixed
// per-image budget after scaling).
func (c *Client) VisionAnalyze(ctx context.Context, imageBase64, mediaType, prompt string) (string, ClaudeUsage, error) {
	if prompt == "" {
		prompt = "描述这张图片，并识别其中的文字（如有）"
	}
	model := ResolveVisionModel(nil) // env overrides at call time
	req := &ClaudeRequest{
		Model:     model,
		MaxTokens: DefaultVisionMaxTokens,
		Messages: []ClaudeMessage{
			{
				Role: "user",
				Content: []ClaudeContentBlock{
					{Type: "image", Source: &ClaudeImageSource{Type: "base64", MediaType: mediaType, Data: imageBase64}},
					{Type: "text", Text: prompt},
				},
			},
		},
	}
	resp, err := c.ClaudeChat(ctx, req)
	if err != nil {
		return "", ClaudeUsage{}, err
	}
	return resp.Text(), resp.Usage, nil
}

// claudeMessagesURL returns the full Claude Messages API URL for the
// configured base URL. Handles ANTHROPIC_BASE_URL both with and without the
// "/anthropic" prefix.
//
// A non-empty MessagesPath (settings.json "messages_path") takes precedence
// and is appended verbatim to the base URL — this is the escape hatch for
// providers whose Anthropic-compatible endpoint does not follow the
// "/anthropic" conventions, e.g. Tencent hy4-preview whose only valid path
// is https://tokenhub.tencentmaas.com/v1/messages (verified live 2026-08-28:
// Bearer + anthropic-version headers pass, model=hy4-preview returned; both
// legacy derivations 404).
//
// Path compatibility (when MessagesPath is empty): the standard Anthropic
// wire convention is {base}/v1/messages; a provider's ANTHROPIC_BASE_URL
// that ends in "/anthropic" (e.g. DeepSeek's
// https://api.deepseek.com/anthropic or GLM's
// https://open.bigmodel.cn/api/anthropic) EXPECTS the client to append
// "/v1/messages". DeepSeek tolerates both /anthropic/v1/messages and the
// legacy /anthropic/messages (both return HTTP 200); GLM rejects
// /anthropic/messages with a 200 + {"code":500,"msg":"404 NOT_FOUND"} body —
// an empty-body stream that collapses into a 0-token run. So we always append
// "/v1/messages" to an "/anthropic"-suffixed base: it is the correct standard
// path and works against both providers (verified live 2026-08-27).
func (c *Client) claudeMessagesURL() string {
	base := strings.TrimRight(c.BaseURL, "/")
	if c.MessagesPath != "" {
		return base + c.MessagesPath
	}
	if strings.HasSuffix(base, claudeAPIPath) {
		return base + "/v1/messages"
	}
	return base + claudeMessagesPath
}

// setClaudeHeaders sets the headers required by the Claude Messages API.
//
// The beta header is CONDITIONAL and it is set by the request, not by the
// client: a Messages body that references an uploaded file (an image block
// whose source type is "file") is refused with HTTP 400 "Missing or invalid
// anthropic-beta header" unless the request carries
// `anthropic-beta: files-api-2025-04-14` (measured 2026-11-06 — the same body
// with the header answers 200).
//
// It is APPENDED through addBetaHeader, never Set: no other beta feature was
// installed on this client when this was written, and a future one must keep
// working — overwriting the header would silently disable whichever feature set
// it first. The header is also absent when no file is referenced, so a
// text-only turn keeps the byte-identical wire it had before the Files API
// existed.
func (c *Client) setClaudeHeaders(req *http.Request, creq *ClaudeRequest) {
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(claudeVersionHeader, claudeAPIVersion)
	if HasFileBlocks(creq) {
		addBetaHeader(req.Header, filesAPIBetaFilesAPI)
	}
}

// claudeChatLadderRoundtrip performs ONE raw non-streaming send and returns
// the HTTP status and body verbatim (err is reserved for transport failures —
// an HTTP 4xx/5xx is DATA to the ladder, not an error). retryDo is NOT used:
// it collapses non-retryable HTTP failures into a synthesized error and
// discards the status/body the ladder needs to classify 1214s.
func (c *Client) claudeChatLadderRoundtrip(ctx context.Context, req *ClaudeRequest) (int, []byte, error) {
	req.Stream = false
	reqBody, err := json.Marshal(req)
	if err != nil {
		return 0, nil, fmt.Errorf("deepseek: failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.claudeMessagesURL(), bytes.NewReader(reqBody))
	if err != nil {
		return 0, nil, fmt.Errorf("deepseek: failed to create request: %w", err)
	}
	c.setClaudeHeaders(httpReq, req)
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(reqBody)), nil
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		c.recordClientFailure(0, nil, false, err)
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		c.recordClientFailure(resp.StatusCode, nil, false, err)
		return resp.StatusCode, nil, err
	}
	if resp.StatusCode != http.StatusOK {
		c.recordClientFailure(resp.StatusCode, body, false, nil)
	}
	if c.RawLog != nil {
		c.RawLog(rawlogfmt.TagProbeRequest, rawlogfmt.ProbeAnnotation, reqBody)
		c.RawLog(rawlogfmt.TagResponse, rawlogfmt.ProbeAnnotation, body)
	}
	return resp.StatusCode, body, nil
}

// ErrEmptyMessages is returned by the outbound choke point when a request
// carries NO messages. It is a client-side refusal, not a provider error: the
// Claude Messages API has no valid request with an empty array, and the field
// carries no `omitempty`, so an empty slice reaches the wire as "messages":
// null and the endpoint answers HTTP 422 "invalid type: null, expected a
// sequence" (measured 2026-09-30, dumps/20260930-194502-1.dump.json — the
// caller then spent a minimal-core degradation re-send guessing a provider
// fault for what was a locally-mangled request).
//
// The refusal lives at the choke point, not only at the trimmer that caused it:
// an empty array is a broken request whoever produced it, and a provider round
// trip is the most expensive way to discover it.
var ErrEmptyMessages = errors.New("deepseek: request carries no messages — the wire field would serialize as messages:null and the endpoint rejects it (HTTP 422); refusing to send")

// validateMessagesNonEmpty is the outbound gate for ErrEmptyMessages. A
// request that passed the tool-pairing invariant can still be empty (an empty
// array trivially satisfies pairing), so this is its own check.
func validateMessagesNonEmpty(req *ClaudeRequest) error {
	if req == nil || len(req.Messages) == 0 {
		return ErrEmptyMessages
	}
	return nil
}

// ClaudeChat sends a non-streaming Claude Messages request and returns the
// accumulated response. DETERMINISTIC 4xx (provider-neutral): on any 4xx
// (except 429) the request is retried ONCE as the minimal core — all
// optional/extension parameters stripped. Still 4xx → fail fast with the
// full request dumped. See minimal_core.go / error_matrix.go.
func (c *Client) ClaudeChat(ctx context.Context, req *ClaudeRequest) (*ClaudeResponse, error) {
	// An empty message array is a locally-broken request — refuse it before
	// the wire (see ErrEmptyMessages).
	if err := validateMessagesNonEmpty(req); err != nil {
		return nil, err
	}
	// Pairing invariant — see ClaudeChatStream for the full rationale and the
	// measured incident. Verified AND repaired at the choke point; every
	// request is gated here, whoever built it.
	// NOTE: a repair is reported through RepairToolUsePairs' own log line, never
	// through recordClientFailure — that snapshot is what the crash dump reads
	// to CATEGORIZE the run's failure (HasFailed/Snapshot), so recording a
	// successful repair there would mark a healthy run as an HTTP error with an
	// empty status and hand the operator a wrong diagnosis.
	pairing := RepairToolUsePairs(req)
	_ = pairing
	if err := ValidateToolUsePairs(req); err != nil {
		return nil, fmt.Errorf("deepseek: refusing to send a request that violates the tool_use/tool_result pairing invariant: %w", err)
	}

	req.Stream = false

	// lastHTTPStatus/Body capture the FINAL HTTP status+body of the retry
	// loop, including non-retryable failures that retryDo collapses into a
	// synthesized error — the 1214 guard needs the raw body to classify it.
	var lastHTTPStatus int
	var lastHTTPBody []byte
	// sentReqBody is the REQUEST BYTES OF THE LAST ATTEMPT — the very slice
	// handed to bytes.NewReader, recorded inside the closure because that is
	// the only place a serialization actually goes out. The raw log below
	// writes THIS slice instead of re-marshaling req: a second json.Marshal
	// yields a second serialization joined to the first only by the accident
	// that the caller does not touch req between the send and the log write,
	// and "logged bytes == wire bytes" must not rest on a coincidence.
	// retryDo RE-MARSHALS PER ATTEMPT (deliberately: each attempt owns its own
	// ctx and httpReq), so the retained slice is the last one, not the first —
	// and a slice json.Marshal allocated is never written to again, so a later
	// mutation of req cannot reach the bytes already sent.
	var sentReqBody []byte
	operation := func() (*http.Response, error) {
		reqBody, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("deepseek: failed to marshal request: %w", err)
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.claudeMessagesURL(), bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("deepseek: failed to create request: %w", err)
		}
		sentReqBody = reqBody
		c.setClaudeHeaders(httpReq, req)
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(reqBody)), nil
		}
		resp, err := c.HTTPClient.Do(httpReq)
		if err == nil {
			lastHTTPStatus = resp.StatusCode
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, non200BodyProbeMax)) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
				resp.Body.Close()
				lastHTTPBody = body
				c.non200RawLog("non-streaming attempt", resp.StatusCode, body) // observability: status + error body
				c.recordClientFailure(resp.StatusCode, body, false, nil)       // crash-dump: error detail collection (bug037)
				return nil, &sentinelAPIError{status: resp.StatusCode, body: body, retryAfter: parseRetryAfter(resp), providerBase: c.BaseURL}
			}
		} else {
			c.recordClientFailure(0, nil, false, err)
		}
		return resp, err
	}

	resp, err := c.retryDo(ctx, operation)
	if err != nil {
		// DETERMINISTIC 4xx (provider-neutral): one minimal-core degradation
		// re-send — strip all optional/extension params, keep the greatest
		// common denominator every LLM API accepts. Still 4xx → fail fast
		// with the request dumped. See minimal_core.go / error_matrix.go.
		// ACCOUNT-LEVEL gate: a 402 billing failure (or a vendor account code
		// inside a 4xx) is never retried AND never degraded — the request is
		// fine, the account is not. Fail fast with a status-carrying error so
		// the caller can skip a same-provider fallback and print the remedy.
		if isAccountLevelStatus(lastHTTPStatus, lastHTTPBody) {
			return nil, accountLevelError(&sentinelAPIError{status: lastHTTPStatus, body: lastHTTPBody, providerBase: c.BaseURL})
		}
		if isDeterministicHTTPError(lastHTTPStatus, nil) {
			log.Printf("%s non-streaming path hit HTTP %d — minimal-core degradation", minimalCoreLogTag, lastHTTPStatus)
			attempts := 0
			var lastRoundtripBody []byte
			var lastRoundtripStatus int
			ladderSend := func(lctx context.Context, lreq *ClaudeRequest) (int, []byte, error) {
				attempts++
				if attempts == 1 {
					// Reuse the already-observed 4xx for the baseline send.
					return lastHTTPStatus, lastHTTPBody, nil
				}
				st, bd, serr := c.claudeChatLadderRoundtrip(lctx, lreq)
				lastRoundtripStatus, lastRoundtripBody = st, bd
				return st, bd, serr
			}
			variant, lerr := c.runMinimalCoreDegrade(ctx, req, ladderSend)
			if lerr != nil {
				return nil, lerr
			}
			// The accepted variant's response was already read by the final
			// ladder roundtrip — parse it directly, no extra send.
			return c.parseClaudeChatResponse(variant, lastRoundtripStatus, lastRoundtripBody)
		}
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("deepseek: failed to read response body: %w", err)
	}

	// Raw log: request + response bodies (single-call, non-streaming).
	// The request bytes are the ones the attempt ACTUALLY sent (sentReqBody),
	// never a fresh json.Marshal(req): that second serialization is a different
	// array of bytes whose agreement with the wire rests on nothing.
	if c.RawLog != nil {
		if len(sentReqBody) > 0 {
			c.RawLog(rawlogfmt.TagRequest, "", sentReqBody)
		}
		c.RawLog(rawlogfmt.TagResponse, "", respBody)
	}

	var wire claudeWireResponse
	if err := json.Unmarshal(respBody, &wire); err != nil {
		return nil, fmt.Errorf("deepseek: failed to unmarshal response: %w", err)
	}

	return &ClaudeResponse{
		ID:         wire.ID,
		Content:    wire.Content,
		StopReason: wire.StopReason,
		Usage:      wire.Usage,
	}, nil
}

// parseClaudeChatResponse parses a non-streaming Claude response body that
// the 1214 guard has already read (status/body captured by the final ladder
// roundtrip).
func (c *Client) parseClaudeChatResponse(req *ClaudeRequest, status int, body []byte) (*ClaudeResponse, error) {
	if status != http.StatusOK || body == nil {
		// Unexpected shape — surface as an API error (the guard only calls
		// this after a 200 was observed).
		return nil, fmt.Errorf("deepseek: API error (status %d): %s", status, tailBytes(body, maxErrorDisplayBytes))
	}
	var wire claudeWireResponse
	if err := json.Unmarshal(body, &wire); err != nil {
		return nil, fmt.Errorf("deepseek: failed to unmarshal response: %w", err)
	}
	return &ClaudeResponse{
		ID:         wire.ID,
		Content:    wire.Content,
		StopReason: wire.StopReason,
		Usage:      wire.Usage,
	}, nil
}

// ClaudeChatStream sends a streaming Claude Messages request and returns a
// channel of parsed SSE events. The channel is closed when the stream ends
// or an error occurs; the caller should read until close and check
// ev.Error for stream-level failures.
func (c *Client) ClaudeChatStream(ctx context.Context, req *ClaudeRequest) (<-chan ClaudeStreamEvent, error) {
	// The Claude Messages pairing invariant is verified AND repaired here —
	// the single outbound choke point every request passes through, whichever
	// layer built it (the ReAct loop's BuildClaudeRequest, the resume path's
	// re-seeded history, the compact summarizer, the chat surface, the
	// openai-proxy fallback). Asserting the invariant in the converters was
	// the old design; it is not assertable, because the array can be damaged
	// by the harness's OWN context management (measured 2026-09-22: the
	// committed-working-region trim dropped the window holding a head
	// tool_use's result → HTTP 400 "messages.1: `tool_use` ids were found
	// without `tool_result` blocks immediately after", the live run died and
	// the damaged array was persisted into the raw-log checkpoint, so every
	// resume died on its first request too). So the invariant is PROVEN here:
	// the repair runs first, then the gate re-reads the array and refuses to
	// send anything the repair could not fix. Nothing reaches the wire
	// unpaired, and nothing damaged is what the raw log (the resume
	// checkpoint) records.
	// A repair means the shipped array was DAMAGED before this point. It is
	// reported through RepairToolUsePairs' own log line (and the returned
	// orphan ids), NEVER through recordClientFailure: that snapshot is what the
	// crash dump reads to CATEGORIZE the run (HasFailed/Snapshot), so writing a
	// successful repair into it would mark a healthy run as an HTTP failure
	// with an empty status and give the operator a wrong diagnosis.
	// An empty message array is a locally-broken request — refuse it before
	// the wire (see ErrEmptyMessages).
	if err := validateMessagesNonEmpty(req); err != nil {
		return nil, err
	}
	if pairing := RepairToolUsePairs(req); pairing.Touched() {
		log.Printf("%s stream choke point repaired %d id(s) before send", toolUseOrphanRepairLogTag, len(pairing.OrphanIDs))
	}
	if err := ValidateToolUsePairs(req); err != nil {
		return nil, fmt.Errorf("deepseek: refusing to send a request that violates the tool_use/tool_result pairing invariant: %w", err)
	}

	req.Stream = true

	// lastHTTPStatus/Body capture the final HTTP status+body of the retry
	// loop for the 1214 guard (same pattern as the non-streaming path).
	var lastHTTPStatus int
	var lastHTTPBody []byte
	// sentReqBody: the request bytes of the last attempt (see ClaudeChat for
	// the full rationale). This path records what it SENT, including
	// `"stream":true`, instead of re-serializing req afterwards.
	var sentReqBody []byte
	operation := func() (*http.Response, error) {
		reqBody, err := json.Marshal(req)
		if err != nil {
			return nil, fmt.Errorf("deepseek: failed to marshal request: %w", err)
		}
		httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			c.claudeMessagesURL(), bytes.NewReader(reqBody))
		if err != nil {
			return nil, fmt.Errorf("deepseek: failed to create request: %w", err)
		}
		sentReqBody = reqBody
		c.setClaudeHeaders(httpReq, req)
		httpReq.Header.Set("Accept", "text/event-stream")
		httpReq.GetBody = func() (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(reqBody)), nil
		}
		resp, err := c.HTTPClient.Do(httpReq)
		if err == nil {
			lastHTTPStatus = resp.StatusCode
			if resp.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(io.LimitReader(resp.Body, non200BodyProbeMax)) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
				resp.Body.Close()
				lastHTTPBody = body
				c.non200RawLog("streaming attempt", resp.StatusCode, body) // observability: status + error body
				c.recordClientFailure(resp.StatusCode, body, true, nil)    // crash-dump: error detail collection (bug037)
				return nil, &sentinelAPIError{status: resp.StatusCode, body: body, retryAfter: parseRetryAfter(resp), providerBase: c.BaseURL}
			}
		} else {
			c.recordClientFailure(0, nil, true, err)
		}
		return resp, err
	}

	resp, err := c.retryDo(ctx, operation)
	if err != nil {
		// DETERMINISTIC 4xx on the STREAMING path: no client-side minimal-core
		// re-send here. The agent layer owns the stream→non-stream fallback
		// (the standard recovery for "stream not supported" 4xx); a client-side
		// re-send of a still-streaming request would double-send and pre-empt
		// that fallback. The request is dumped for observability; the error is
		// surfaced verbatim. The NON-streaming path (ClaudeChat) owns the
		// minimal-core degradation — if the fallback also 4xxs, it degrades
		// there, exactly once.
		if isDeterministicHTTPError(lastHTTPStatus, nil) {
			c.non200RawLog("streaming deterministic 4xx (fail-fast, fallback owns recovery)", lastHTTPStatus, lastHTTPBody)
			if dumpPath := dumpFailedRequest(req, lastHTTPStatus, lastHTTPBody); dumpPath != "" {
				log.Printf("%s streaming HTTP %d — request dump: %s (non-stream fallback owns recovery)", minimalCoreLogTag, lastHTTPStatus, dumpPath)
			}
		}
		return nil, err
	}

	// Raw log: request body (streaming — events are logged individually).
	// sentReqBody is the slice the attempt put on the wire (see ClaudeChat).
	if c.RawLog != nil && len(sentReqBody) > 0 {
		c.RawLog(rawlogfmt.TagRequest, "", sentReqBody)
	}

	// retryDo returns the open body on success — parseClaudeSSE takes
	// ownership and closes it.
	ch := make(chan ClaudeStreamEvent)
	go c.parseClaudeSSE(ctx, resp.Body, ch)
	return ch, nil
}

// claudeStreamLadderRoundtrip performs ONE raw streaming send and returns
// the HTTP status and body verbatim (err reserved for transport failures —
// an HTTP 4xx/5xx is DATA to the ladder, not an error). retryDo is NOT used
// (same reason as claudeChatLadderRoundtrip).
func (c *Client) claudeStreamLadderRoundtrip(ctx context.Context, req *ClaudeRequest) (int, []byte, error) {
	req.Stream = true
	reqBody, err := json.Marshal(req)
	if err != nil {
		return 0, nil, fmt.Errorf("deepseek: failed to marshal request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.claudeMessagesURL(), bytes.NewReader(reqBody))
	if err != nil {
		return 0, nil, fmt.Errorf("deepseek: failed to create request: %w", err)
	}
	c.setClaudeHeaders(httpReq, req)
	httpReq.Header.Set("Accept", "text/event-stream")
	httpReq.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(bytes.NewReader(reqBody)), nil
	}
	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		c.recordClientFailure(0, nil, true, err)
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, non200BodyProbeMax)) //stylegate:ignore bounded read of an error/diagnostic body; a short read is acceptable
	if resp.StatusCode != http.StatusOK {
		c.recordClientFailure(resp.StatusCode, body, true, nil)
	}
	if c.RawLog != nil {
		c.RawLog(rawlogfmt.TagProbeRequest, rawlogfmt.ProbeStreamAnnotation, reqBody)
		if resp.StatusCode != http.StatusOK {
			c.RawLog(rawlogfmt.TagResponse, rawlogfmt.ProbeStreamAnnotation, body)
		}
	}
	return resp.StatusCode, body, nil
}

// parseClaudeSSE reads an SSE stream from r and sends parsed
// ClaudeStreamEvents to ch. It closes ch when the stream ends or an
// unrecoverable error occurs.
func (c *Client) parseClaudeSSE(ctx context.Context, r io.ReadCloser, ch chan<- ClaudeStreamEvent) {
	defer close(ch)
	defer r.Close()

	var ev ClaudeStreamEvent         // reused across iterations
	acc := newClaudeStreamResponse() // folds the same events into one record
	err := scanSSEEvents(ctx, r, func(event []byte) {
		c.emitClaudeEvent(ctx, ch, &ev, event, acc)
	})
	// THE COMPLETE RESPONSE RECORD, written when the stream ENDS — however it
	// ended (bug 01a19bf1-ff52). It is written AFTER every chunk record of this
	// request (they are written per event, above), so the log reads
	// R, C…C, S — one full response per request, which is what the non-streaming
	// path writes and what a reader pairs on.
	//
	// The `complete` flag distinguishes a whole body from a truncated one, and the
	// witness is the server's own message_stop — NOT the reader's error. A
	// connection that dies mid-stream ends in the same clean io.EOF a finished
	// stream does, so "no error" proves nothing; the walker flushes a buffered
	// partial event and returns nil, which is exactly the case that would be
	// mislabelled. A stream that stopped early still yields a record (the usage
	// from message_start is real and worth a reader's time) marked partial, so a
	// caller is never told a body arrived whole when it did not.
	streamComplete := acc.sawStop && ctx.Err() == nil
	c.logStreamResponse(acc, streamComplete)
	if err != nil && err != io.EOF && err != context.Canceled {
		select {
		case ch <- ClaudeStreamEvent{Type: "error", Error: fmt.Sprintf("SSE reader error: %v", err)}:
		case <-ctx.Done():
		}
	}
}

// emitClaudeEvent parses a raw SSE event payload and, if it contains valid
// data, sends a ClaudeStreamEvent to ch. The full event (event: and data:
// lines, prefixes preserved) is written to the raw log for replayability.
func (c *Client) emitClaudeEvent(ctx context.Context, ch chan<- ClaudeStreamEvent, ev *ClaudeStreamEvent, event []byte, acc *claudeStreamResponse) {
	var dataPayload []byte
	var rawEvent []byte
	for _, eventLine := range bytes.Split(event, []byte("\n")) {
		// Skip comment-only lines.
		if bytes.HasPrefix(eventLine, []byte(":")) {
			continue
		}
		rawEvent = append(rawEvent, eventLine...)
		rawEvent = append(rawEvent, '\n')
		if bytes.HasPrefix(eventLine, []byte("data:")) {
			d := bytes.TrimSpace(bytes.TrimPrefix(eventLine, []byte("data:")))
			if len(dataPayload) > 0 {
				dataPayload = append(dataPayload, '\n')
			}
			dataPayload = append(dataPayload, d...)
		}
	}

	if len(dataPayload) == 0 {
		return
	}

	// Raw log: each SSE event, prefixes preserved so the transcript is
	// complete and replayable.
	if c.RawLog != nil {
		c.RawLog(rawlogfmt.TagChunk, "", rawEvent)
	}

	var wire claudeWireEvent
	if err := json.Unmarshal(dataPayload, &wire); err != nil {
		// Skip unparseable chunks silently — the caller can check for
		// missing data.
		return
	}

	// The SAME event folded into this call's complete-response record. Done HERE,
	// beside the parse, so the two views of one stream cannot drift: the channel
	// event and the recorded body are both derived from this one decode.
	if acc != nil {
		acc.observe(&wire)
	}

	*ev = ClaudeStreamEvent{Type: wire.Type, Index: wire.Index}
	switch wire.Type {
	case "content_block_start":
		if wire.ContentBlock.Type != "" {
			b := wire.ContentBlock
			ev.Block = &b
		}
	case "content_block_delta":
		ev.Delta = &ClaudeStreamDelta{
			Type:        wire.Delta.Type,
			Text:        wire.Delta.Text,
			Thinking:    wire.Delta.Thinking,
			PartialJSON: wire.Delta.PartialJSON,
			Signature:   wire.Delta.Signature,
		}
	case "message_start":
		if wire.Message != nil {
			ev.MessageID = wire.Message.ID
			ev.Usage = wire.Message.Usage
		}
	case "message_delta":
		ev.StopReason = wire.Delta.StopReason
		if wire.Usage != nil {
			ev.Usage = *wire.Usage
		}
	case "error":
		if wire.Error != nil {
			ev.Error = wire.Error.Message
		}
	}

	select {
	case ch <- *ev:
	case <-ctx.Done():
	}
}

// claudeWireEvent mirrors the JSON payload of a single Claude SSE data line.
type claudeWireEvent struct {
	Type         string             `json:"type"`
	Index        int                `json:"index"`
	ContentBlock ClaudeContentBlock `json:"content_block"`
	Delta        claudeWireDelta    `json:"delta"`
	Message      *claudeWireMessage `json:"message"`
	Usage        *ClaudeUsage       `json:"usage"`
	Error        *claudeWireError   `json:"error"`
}

// claudeWireDelta is the delta object shared by content_block_delta and
// message_delta events.
type claudeWireDelta struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Thinking    string `json:"thinking"`
	PartialJSON string `json:"partial_json"`
	Signature   string `json:"signature"`
	StopReason  string `json:"stop_reason"`
}

// claudeWireMessage is the message object of a message_start event.
type claudeWireMessage struct {
	ID    string      `json:"id"`
	Usage ClaudeUsage `json:"usage"`
}

// claudeWireError is the error object of an error event.
type claudeWireError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

// claudeWireResponse is the JSON body of a non-streaming Claude response.
type claudeWireResponse struct {
	ID         string               `json:"id"`
	Content    []ClaudeContentBlock `json:"content"`
	StopReason string               `json:"stop_reason"`
	Usage      ClaudeUsage          `json:"usage"`
}

// PromptSideTokens is the PROMPT side of a usage record: the tokens the
// request cost on the way IN — fresh input plus cache writes plus cache reads.
//
// It exists as ONE named accessor because the same three-term sum decides two
// different things in two different layers, and a second spelling of it is a
// second rule (Rules 1/13/14):
//
//   - it is what Context.SetPromptTokens is fed with (the measured prompt
//     size), and
//   - it is what AddClaudeUsage accumulates into TotalPromptTokens (cost).
//
// OutputTokens is deliberately NOT part of it: the prompt side is what the
// context estimate is about, and the completion belongs to the next round's
// input once it is replayed into the history.
//
// IT SITS AT THE END OF THE FILE ON PURPOSE, and the reason is measured, not
// stylistic: this file's earlier lines are cited by the docs of record
// (devdoc/plain-prompt-first-user-message-scope.md -> claude_client.go:560,
// devdoc/gopi-chat-clarify-landing.md -> :481), and a landed record is never
// re-keyed, so inserting N lines anywhere above them turns a settled citation
// into a stale one that only a hand edit repairs. Appending shifts nothing
// (measured: placing these two functions beside the type moved :481/…:560 and
// produced exactly one new drift each; appended at the end, nothing above them
// moved).
//
// HISTORY: the drift used to be COUNTED by the deleted devdoc line-anchor gate and
// reported against a baseline ("the same drift count as master"). That gate and
// its baseline were deleted with internal/docclass in 5a35f61, so nothing counts
// a devdoc line anchor now — the reason above is churn, and the census that
// replaced it over CONFIG.md's rows (`config-anchor-census.go --check`) went with
// the documentation-citation family in 78bdf05a (2026-11-05): nothing executable
// checks an anchor on this tree.
func (u ClaudeUsage) PromptSideTokens() int {
	return u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens
}

// HasPromptMeasurement reports whether this usage record carries a prompt-side
// measurement at all. A frame with no counters (or an all-zero one) is an
// ABSENCE of measurement, never a measurement of zero — writing it into the
// context would replace a real count with a fabricated 0 (Rule 15).
func (u ClaudeUsage) HasPromptMeasurement() bool {
	return u.PromptSideTokens() > 0
}
