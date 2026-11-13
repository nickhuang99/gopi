package deepseek

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/rawlogfmt"
)

// ---------------------------------------------------------------------------
// BuildClaudeRequest conversion rules
// ---------------------------------------------------------------------------

func TestBuildClaudeRequest_SystemBlocksAndRoles(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "kernel"}, // promoted to top-level
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
	}
	req := BuildClaudeRequest("deepseek-v4-flash", msgs, "You are GoPi", "project ctx", nil, DefaultClaudeMaxTokens, nil)

	if len(req.System) != 2 {
		t.Fatalf("expected 2 system blocks, got %d", len(req.System))
	}
	if req.System[0].Text != "You are GoPi" || req.System[1].Text != "project ctx" {
		t.Errorf("unexpected system blocks: %+v", req.System)
	}
	if len(req.Messages) != 2 {
		t.Fatalf("expected 2 messages (system dropped), got %d", len(req.Messages))
	}
	if req.Messages[0].Role != "user" || req.Messages[0].Content[0].Type != "text" {
		t.Errorf("unexpected first message: %+v", req.Messages[0])
	}
	if req.Messages[1].Role != "assistant" || req.Messages[1].Content[0].Text != "hi there" {
		t.Errorf("unexpected second message: %+v", req.Messages[1])
	}
	if req.MaxTokens != DefaultClaudeMaxTokens {
		t.Errorf("expected max_tokens %d, got %d", DefaultClaudeMaxTokens, req.MaxTokens)
	}
}

func TestBuildClaudeRequest_ThinkingReplay(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "let me check", ReasoningContent: "I should read the file", ThinkingSignature: "sig_abc", ToolCalls: []ToolCall{{ID: "call_1", Name: "read", Arguments: map[string]interface{}{"path": "/tmp/x"}}}},
		{Role: "tool", Content: "file content", ToolCallID: "call_1"},
	}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}
	asst := req.Messages[1]
	if asst.Role != "assistant" {
		t.Fatalf("expected assistant message, got %q", asst.Role)
	}
	// Order: thinking → text → tool_use
	if len(asst.Content) != 3 {
		t.Fatalf("expected 3 content blocks, got %d", len(asst.Content))
	}
	if asst.Content[0].Type != "thinking" || asst.Content[0].Thinking != "I should read the file" || asst.Content[0].Signature != "sig_abc" {
		t.Errorf("unexpected thinking block: %+v", asst.Content[0])
	}
	if asst.Content[1].Type != "text" || asst.Content[1].Text != "let me check" {
		t.Errorf("unexpected text block: %+v", asst.Content[1])
	}
	if asst.Content[2].Type != "tool_use" || asst.Content[2].ID != "call_1" || asst.Content[2].Name != "read" {
		t.Errorf("unexpected tool_use block: %+v", asst.Content[2])
	}
	// Tool result → user message with tool_result block.
	last := req.Messages[2]
	if last.Role != "user" || len(last.Content) != 1 || last.Content[0].Type != "tool_result" || last.Content[0].ToolUseID != "call_1" {
		t.Errorf("unexpected tool result message: %+v", last)
	}
}

// TestBuildClaudeRequest_EmptyThinkingDropped covers bug024: DeepSeek
// sometimes streams an EMPTY thinking block (no thinking text, signature may
// be a bogus message id). Replaying it breaks the wire format — Go's
// omitempty drops the empty `thinking` value, leaving {"type":"thinking"},
// which the DeepSeek deserializer rejects with HTTP 400 "missing field
// `thinking`". Empty thinking blocks must be dropped from history entirely;
// the text/tool_use blocks survive.
func TestBuildClaudeRequest_EmptyThinkingDropped(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ReasoningContent: "", ThinkingSignature: "1338495e-0435-4ff7-af8e-6a1dfee223e2", ToolCalls: []ToolCall{{ID: "call_1", Name: "bash", Arguments: map[string]interface{}{"command": "ls"}}}},
		{Role: "tool", Content: "exit=0\n", ToolCallID: "call_1"},
	}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(req.Messages))
	}
	asst := req.Messages[1]
	// Empty thinking block must not be replayed — the wire must not contain
	// a thinking block whose `thinking` field was dropped by omitempty.
	for _, b := range asst.Content {
		if b.Type == "thinking" {
			t.Fatalf("empty thinking block replayed: %+v", b)
		}
	}
	if len(asst.Content) != 1 || asst.Content[0].Type != "tool_use" || asst.Content[0].ID != "call_1" {
		t.Errorf("expected only the tool_use block, got %+v", asst.Content)
	}
	// A non-empty thinking block with an empty signature (interrupted
	// stream) is still replayed — DeepSeek accepts it.
	msgs2 := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ReasoningContent: "half a thought", ThinkingSignature: "", ToolCalls: nil},
	}
	req2 := BuildClaudeRequest("m", msgs2, "", "", nil, DefaultClaudeMaxTokens, nil)
	asst2 := req2.Messages[1]
	if len(asst2.Content) != 1 || asst2.Content[0].Type != "thinking" || asst2.Content[0].Thinking != "half a thought" {
		t.Errorf("expected thinking block replayed, got %+v", asst2.Content)
	}
}

// TestBuildClaudeRequest_ToolUseInputAlwaysPresent covers the agent's
// replay path: a tool call whose arguments are nil/empty must still marshal
// with an `input` key — DeepSeek's deserializer rejects tool_use blocks
// without it (HTTP 400 "missing field `input`", verified live).
func TestBuildClaudeRequest_ToolUseInputAlwaysPresent(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{{ID: "call_1", Name: "bash", Arguments: nil}}},
		{Role: "tool", Content: "done", ToolCallID: "call_1"},
	}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(b, []byte(`"input":{}`)) {
		t.Fatalf("tool_use block lost its input field: %s", b)
	}
}

func TestBuildClaudeRequest_CoalescesConsecutiveToolResults(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "q"},
		{Role: "assistant", Content: "", ToolCalls: []ToolCall{
			{ID: "c1", Name: "read", Arguments: map[string]interface{}{}},
			{ID: "c2", Name: "bash", Arguments: map[string]interface{}{}},
		}},
		{Role: "tool", Content: "result1", ToolCallID: "c1"},
		{Role: "tool", Content: "result2", ToolCallID: "c2"},
	}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	if len(req.Messages) != 3 {
		t.Fatalf("expected 3 messages (2 tool results coalesced), got %d", len(req.Messages))
	}
	last := req.Messages[2]
	if last.Role != "user" {
		t.Fatalf("expected user message, got %q", last.Role)
	}
	if len(last.Content) != 2 {
		t.Fatalf("expected 2 tool_result blocks, got %d", len(last.Content))
	}
	if last.Content[0].ToolUseID != "c1" || last.Content[1].ToolUseID != "c2" {
		t.Errorf("unexpected tool_result order: %+v", last.Content)
	}
}

func TestBuildClaudeRequest_MergesConsecutiveUserMessages(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "first"},
		{Role: "user", Content: "second"},
	}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 merged user message, got %d", len(req.Messages))
	}
	if len(req.Messages[0].Content) != 2 {
		t.Fatalf("expected 2 text blocks, got %d", len(req.Messages[0].Content))
	}
}

func TestBuildClaudeRequest_DropsEmptyMessages(t *testing.T) {
	msgs := []Message{
		{Role: "user", Content: "   "},
		{Role: "assistant", Content: "", ToolCalls: nil},
		{Role: "user", Content: "real"},
	}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	if len(req.Messages) != 1 {
		t.Fatalf("expected only the non-empty user message, got %d messages", len(req.Messages))
	}
	if req.Messages[0].Content[0].Text != "real" {
		t.Errorf("unexpected message: %+v", req.Messages[0])
	}
}

// TestBuildClaudeRequest_ImageBlock is the wire-level proof of the Epic's
// theory: an image attached to a user message is emitted as a native
// {"type":"image","source":{...}} content block in the main loop request —
// the vision model receives the pixels directly (image tokens = text tokens),
// NOT an OCR-extracted text description. Also verifies the empty-content
// drop rule still applies (an image-only user message is NOT dropped), that a
// user message with text + image emits image first then text, and that
// CONSECUTIVE user messages — image-carrying ones included — are merged into
// one wire message so roles strictly alternate.
func TestBuildClaudeRequest_ImageBlock(t *testing.T) {
	img := &ClaudeImageSource{Type: "base64", MediaType: "image/png", Data: "aGVsbG8="}
	msgs := []Message{
		{Role: "user", Content: "what does this show?", Images: []ClaudeImageSource{*img}},
		{Role: "user", Images: []ClaudeImageSource{*img}}, // image-only message must survive (not empty)
		{Role: "user", Content: "   "},                    // plain whitespace still dropped
	}
	req := BuildClaudeRequest("deepseek-v4-flash-vision-exp", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	// ONE user message: the consecutive user messages merge (images included),
	// carrying image, text, image — the whitespace-only one contributes nothing.
	if len(req.Messages) != 1 {
		t.Fatalf("expected 1 merged user message, got %d: %+v", len(req.Messages), req.Messages)
	}
	first := req.Messages[0]
	if first.Role != "user" {
		t.Errorf("Role = %q, want user", first.Role)
	}
	if len(first.Content) != 3 {
		t.Fatalf("merged content blocks = %d, want 3 (image + text + image)", len(first.Content))
	}
	if first.Content[0].Type != "image" {
		t.Errorf("Content[0].Type = %q, want image", first.Content[0].Type)
	}
	if first.Content[0].Source == nil || first.Content[0].Source.Data != "aGVsbG8=" {
		t.Errorf("Content[0].Source = %+v, want base64 data", first.Content[0].Source)
	}
	if first.Content[0].Source.MediaType != "image/png" {
		t.Errorf("Source.MediaType = %q, want image/png", first.Content[0].Source.MediaType)
	}
	if first.Content[1].Type != "text" || first.Content[1].Text != "what does this show?" {
		t.Errorf("Content[1] = %+v, want text instruction", first.Content[1])
	}
	if first.Content[2].Type != "image" {
		t.Errorf("Content[2] = %+v, want the second image block", first.Content[2])
	}
}

// TestBuildClaudeRequest_ImageBlockWireFormat marshals the request and
// verifies the exact wire shape the API receives — the source of truth for
// "the image travels natively in the main loop".
func TestBuildClaudeRequest_ImageBlockWireFormat(t *testing.T) {
	img := &ClaudeImageSource{Type: "base64", MediaType: "image/jpeg", Data: "c2NyZWVuc2hvdA=="}
	msgs := []Message{{Role: "user", Content: "read the text", Images: []ClaudeImageSource{*img}}}
	req := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, nil)

	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	for _, want := range []string{
		`"type":"image"`,
		`"type":"base64"`,
		`"media_type":"image/jpeg"`,
		`"data":"c2NyZWVuc2hvdA=="`,
		`"type":"text"`,
		`"read the text"`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("wire JSON missing %s\n%s", want, s)
		}
	}
}

func TestBuildClaudeRequest_ThinkingParam(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "q"}}

	enabled := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "enabled"})
	if enabled.Thinking == nil || enabled.Thinking.Type != "enabled" || enabled.Thinking.BudgetTokens != DefaultClaudeThinkingBudget {
		t.Errorf("expected enabled thinking with budget %d, got %+v", DefaultClaudeThinkingBudget, enabled.Thinking)
	}

	disabled := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "disabled"})
	if disabled.Thinking == nil || disabled.Thinking.Type != "disabled" || disabled.Thinking.BudgetTokens != 0 {
		t.Errorf("expected disabled thinking without budget, got %+v", disabled.Thinking)
	}
}

// TestBuildClaudeRequest_GLMDisabledThinkingOmitsField pins the GLM thinking
// fix (verified live 2026-08-27): the GLM Anthropic endpoint ALWAYS thinks and
// REJECTS thinking:disabled with HTTP 400 error 1210 ("该模型始终思考，不支持关闭
// 思考；请使用 low、high 或 max"). So for a GLM model requested with thinking
// disabled, the thinking field must be OMITTED entirely (GLM then thinks
// natively and ignores any budget) rather than sent as disabled. DeepSeek
// models must be unaffected — they honor thinking:disabled, so the field
// stays.
func TestBuildClaudeRequest_GLMDisabledThinkingOmitsField(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "q"}}

	// GLM model + thinking disabled → thinking field omitted (nil).
	glm := BuildClaudeRequest("glm-5.3-flash", msgs, "", "", nil, DefaultClaudeMaxTokens,
		&Thinking{Type: "disabled", BudgetTokens: 1024})
	if glm.Thinking != nil {
		t.Errorf("GLM disabled thinking should omit the field, got %+v", glm.Thinking)
	}

	// GLM model + thinking enabled → field present with budget.
	glmOn := BuildClaudeRequest("glm-5.3-flash", msgs, "", "", nil, DefaultClaudeMaxTokens,
		&Thinking{Type: "enabled", BudgetTokens: 1024})
	if glmOn.Thinking == nil || glmOn.Thinking.Type != "enabled" || glmOn.Thinking.BudgetTokens != 1024 {
		t.Errorf("GLM enabled thinking should keep the field+budget, got %+v", glmOn.Thinking)
	}

	// DeepSeek model + thinking disabled → field MUST stay (regression guard:
	// the fix must not leak to DeepSeek, which honors disabled).
	dk := BuildClaudeRequest("deepseek-v4-flash", msgs, "", "", nil, DefaultClaudeMaxTokens,
		&Thinking{Type: "disabled", BudgetTokens: 1024})
	if dk.Thinking == nil || dk.Thinking.Type != "disabled" {
		t.Errorf("DeepSeek disabled thinking must keep the field, got %+v", dk.Thinking)
	}
}

// TestBuildClaudeRequest_ThinkingBudget verifies the thinking budget: a fixed
// modest default (DeepSeek generates thinking at ~65 tok/s, so a big budget
// costs minutes per round), capped below max_tokens, and overridable via
// Thinking.BudgetTokens (GOPI_THINKING_BUDGET). Regression test for the
// max_tokens truncation bug: the model once spent its whole output allowance
// on thinking and never emitted the write tool call.
func TestBuildClaudeRequest_ThinkingBudget(t *testing.T) {
	msgs := []Message{{Role: "user", Content: "q"}}

	small := BuildClaudeRequest("m", msgs, "", "", nil, 4096, &Thinking{Type: "enabled"})
	if small.Thinking.BudgetTokens != DefaultClaudeThinkingBudget {
		t.Errorf("max_tokens=4096 → thinking budget %d, want default %d",
			small.Thinking.BudgetTokens, DefaultClaudeThinkingBudget)
	}

	large := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "enabled"})
	if large.Thinking.BudgetTokens != DefaultClaudeThinkingBudget {
		t.Errorf("max_tokens=%d → thinking budget %d, want default %d",
			DefaultClaudeMaxTokens, large.Thinking.BudgetTokens, DefaultClaudeThinkingBudget)
	}
	if large.Thinking.BudgetTokens >= large.MaxTokens {
		t.Errorf("thinking budget %d must be below max_tokens %d", large.Thinking.BudgetTokens, large.MaxTokens)
	}

	// Explicit budget (GOPI_THINKING_BUDGET): used when below half the cap,
	// never above it.
	low := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "enabled", BudgetTokens: 512})
	if low.Thinking.BudgetTokens != 512 {
		t.Errorf("explicit budget 512 → got %d", low.Thinking.BudgetTokens)
	}
	huge := BuildClaudeRequest("m", msgs, "", "", nil, 4096, &Thinking{Type: "enabled", BudgetTokens: 100000})
	if huge.Thinking.BudgetTokens >= 4096 {
		t.Errorf("budget must be capped below max_tokens, got %d", huge.Thinking.BudgetTokens)
	}
}

// TestBuildClaudeRequest_ToolRoundNeedsThinkingBlock pins the thinking-mode
// wire rule (verified live 2026-08-21): when the request has thinking enabled,
// every assistant message carrying a tool_use MUST also carry a thinking block
// — the API rejects tool rounds whose thinking was dropped with HTTP 400
// "The `content[].thinking` in the thinking mode must be passed back to the
// API". An EMPTY block satisfies the requirement, so the harness injects one
// (with a constant signature) when a tool round has no thinking text to replay
// (generated with thinking disabled — harness budget downgrade — or streamed
// empty — bug024). The thinking field must be PRESENT on the wire (omitempty
// would drop a true empty string), hence the single space.
func TestBuildClaudeRequest_ToolRoundNeedsThinkingBlock(t *testing.T) {
	asst := Message{
		Role:      "assistant",
		Content:   "let me search",
		ToolCalls: []ToolCall{{ID: "call_1", Name: "read", Arguments: map[string]interface{}{"path": "/tmp/x"}}},
	}
	msgs := []Message{{Role: "user", Content: "q"}, asst}

	// Thinking enabled + tool round without thinking → placeholder injected.
	enabled := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "enabled"})
	var asstMsg *ClaudeMessage
	for i := range enabled.Messages {
		if enabled.Messages[i].Role == "assistant" {
			asstMsg = &enabled.Messages[i]
		}
	}
	if asstMsg == nil || len(asstMsg.Content) != 3 {
		t.Fatalf("expected thinking+text+tool_use blocks, got %+v", asstMsg)
	}
	if asstMsg.Content[0].Type != "thinking" || asstMsg.Content[0].Thinking != " " || asstMsg.Content[0].Signature != EmptyThinkingSignature {
		t.Errorf("expected injected empty thinking block first, got %+v", asstMsg.Content[0])
	}

	// Thinking disabled → no placeholder needed (disabled mode allows tool
	// rounds without thinking blocks — verified live).
	disabled := BuildClaudeRequest("m", msgs, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "disabled"})
	for i := range disabled.Messages {
		if disabled.Messages[i].Role == "assistant" {
			asstMsg = &disabled.Messages[i]
		}
	}
	if asstMsg == nil || len(asstMsg.Content) != 2 {
		t.Fatalf("expected text+tool_use blocks without placeholder, got %+v", asstMsg)
	}
	for _, b := range asstMsg.Content {
		if b.Type == "thinking" {
			t.Errorf("unexpected thinking block in disabled-mode request: %+v", b)
		}
	}

	// Real thinking content is replayed as-is (no placeholder).
	withThinking := asst
	withThinking.ReasoningContent = "I should read the file"
	withThinking.ThinkingSignature = "sig_real"
	msgs2 := []Message{{Role: "user", Content: "q"}, withThinking}
	real := BuildClaudeRequest("m", msgs2, "", "", nil, DefaultClaudeMaxTokens, &Thinking{Type: "enabled"})
	for i := range real.Messages {
		if real.Messages[i].Role == "assistant" {
			asstMsg = &real.Messages[i]
		}
	}
	if asstMsg == nil || len(asstMsg.Content) != 3 {
		t.Fatalf("expected thinking+text+tool_use blocks, got %+v", asstMsg)
	}
	if asstMsg.Content[0].Thinking != "I should read the file" || asstMsg.Content[0].Signature != "sig_real" {
		t.Errorf("expected real thinking replayed unchanged, got %+v", asstMsg.Content[0])
	}
}

// ---------------------------------------------------------------------------
// stop_reason mapping + response helpers
// ---------------------------------------------------------------------------

func TestMapClaudeStopReason(t *testing.T) {
	cases := map[string]string{
		"end_turn":      "stop",
		"tool_use":      "tool_calls",
		"max_tokens":    "length",
		"stop_sequence": "stop",
		"refusal":       "content_filter",
		"":              "stop",
		"weird":         "stop",
	}
	for in, want := range cases {
		if got := MapClaudeStopReason(in); got != want {
			t.Errorf("MapClaudeStopReason(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestClaudeResponseHelpers(t *testing.T) {
	resp := &ClaudeResponse{
		Content: []ClaudeContentBlock{
			{Type: "thinking", Thinking: "think 1", Signature: "s1"},
			{Type: "text", Text: "Hello "},
			{Type: "tool_use", ID: "c1", Name: "read", Input: map[string]interface{}{"path": "/x"}},
			{Type: "thinking", Thinking: "think 2"},
			{Type: "text", Text: "world"},
		},
		StopReason: "end_turn",
	}

	if got := resp.Text(); got != "Hello world" {
		t.Errorf("Text() = %q, want %q", got, "Hello world")
	}
	thinking, sig := resp.Thinking()
	if thinking != "think 1think 2" {
		t.Errorf("Thinking() text = %q", thinking)
	}
	if sig != "s1" {
		t.Errorf("Thinking() signature = %q, want s1", sig)
	}

	calls := resp.ToolCalls()
	if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Name != "read" {
		t.Fatalf("ToolCalls() = %+v", calls)
	}
	if calls[0].Arguments["path"] != "/x" {
		t.Errorf("unexpected arguments: %+v", calls[0].Arguments)
	}
}

// ---------------------------------------------------------------------------
// Claude SSE stream parsing
// ---------------------------------------------------------------------------

// TestClaudeParseSSE feeds a realistic Claude SSE transcript (captured from
// the live DeepSeek endpoint) through parseClaudeSSE and verifies the events.
func TestClaudeParseSSE(t *testing.T) {
	transcript := `event: message_start
data: {"type":"message_start","message":{"id":"ab5f4fff","type":"message","role":"assistant","model":"deepseek-v4-flash","content":[],"stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":109,"cache_creation_input_tokens":0,"cache_read_input_tokens":256,"output_tokens":0,"service_tier":"standard"}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"thinking","thinking":"","signature":""}}

event: ping
data: {"type":"ping"}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"thinking_delta","thinking":"The user asks"}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"signature_delta","signature":"ab5f4fff"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: content_block_start
data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_00_io6","name":"read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"path\":"}}

event: content_block_delta
data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"\"/tmp/x.txt\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":1}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":109,"cache_creation_input_tokens":0,"cache_read_input_tokens":256,"output_tokens":76,"service_tier":"standard"}}

event: message_stop
data: {"type":"message_stop"}
`

	var got []ClaudeStreamEvent
	var rawLogs []string
	client := &Client{}
	client.RawLog = func(tag, annotation string, data []byte) {
		if tag == rawlogfmt.TagChunk && annotation == "" {
			rawLogs = append(rawLogs, string(data))
		}
	}
	ch := make(chan ClaudeStreamEvent, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range ch {
			got = append(got, ev)
		}
	}()
	client.parseClaudeSSE(context.Background(), io.NopCloser(strings.NewReader(transcript)), ch)
	<-done

	if len(got) != 12 {
		t.Fatalf("expected 12 events, got %d: %+v", len(got), got)
	}

	// message_start
	if got[0].Type != "message_start" || got[0].MessageID != "ab5f4fff" {
		t.Errorf("message_start: %+v", got[0])
	}
	if got[0].Usage.InputTokens != 109 || got[0].Usage.CacheReadInputTokens != 256 {
		t.Errorf("message_start usage: %+v", got[0].Usage)
	}

	// thinking block start
	if got[1].Type != "content_block_start" || got[1].Block == nil || got[1].Block.Type != "thinking" {
		t.Errorf("thinking start: %+v", got[1])
	}

	// ping
	if got[2].Type != "ping" {
		t.Errorf("ping: %+v", got[2])
	}

	// thinking delta + signature delta
	if got[3].Type != "content_block_delta" || got[3].Delta.Type != "thinking_delta" || got[3].Delta.Thinking != "The user asks" {
		t.Errorf("thinking delta: %+v", got[3])
	}
	if got[4].Delta.Type != "signature_delta" || got[4].Delta.Signature != "ab5f4fff" {
		t.Errorf("signature delta: %+v", got[4])
	}

	// tool_use block + input_json deltas
	if got[6].Block == nil || got[6].Block.Type != "tool_use" || got[6].Block.ID != "call_00_io6" {
		t.Errorf("tool_use start: %+v", got[6])
	}
	if got[7].Delta.PartialJSON != `{"path":` {
		t.Errorf("input_json delta 1: %+v", got[7])
	}
	if got[8].Delta.PartialJSON != `"/tmp/x.txt"}` {
		t.Errorf("input_json delta 2: %+v", got[8])
	}

	// message_delta carries stop_reason + usage
	if got[10].Type != "message_delta" || got[10].StopReason != "tool_use" {
		t.Errorf("message_delta: %+v", got[10])
	}
	if got[10].Usage.OutputTokens != 76 {
		t.Errorf("message_delta usage: %+v", got[10].Usage)
	}

	// Raw log must preserve event: + data: prefixes for replayability.
	if len(rawLogs) != 12 {
		t.Fatalf("expected 12 raw-log entries, got %d", len(rawLogs))
	}
	if !strings.HasPrefix(rawLogs[0], "event: message_start\ndata: ") {
		t.Errorf("raw log entry missing event: prefix: %q", rawLogs[0])
	}
}

// ---------------------------------------------------------------------------
// HTTP integration with a mock Claude server
// ---------------------------------------------------------------------------

// TestClaudeChatStream_HTTPServerIntegration drives ClaudeChatStream against
// an httptest server serving a full tool_use SSE transcript and verifies the
// accumulated response via ClaudeResponse helpers.
func TestClaudeChatStream_HTTPServerIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/anthropic/messages" {
			t.Errorf("expected /anthropic/messages, got %s", r.URL.Path)
		}
		if got := r.Header.Get("anthropic-version"); got != claudeAPIVersion {
			t.Errorf("expected anthropic-version %q, got %q", claudeAPIVersion, got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("expected Authorization header, got %q", got)
		}

		// Verify the request body carries a tool def in Claude format.
		var req ClaudeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if len(req.Tools) == 0 || req.Tools[0].Name != "read" || req.Tools[0].InputSchema == nil {
			t.Errorf("expected Claude-format tool def, got %+v", req.Tools)
		}

		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`event: message_start
data: {"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":0}}}

event: content_block_start
data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"read","input":{}}}

event: content_block_delta
data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"path\":\"/tmp/x\"}"}}

event: content_block_stop
data: {"type":"content_block_stop","index":0}

event: message_delta
data: {"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":5}}

event: message_stop
data: {"type":"message_stop"}
`))
	}))
	defer server.Close()

	client := &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: DefaultRetryConfig(),
	}

	req := &ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: DefaultClaudeMaxTokens,
		Messages: []ClaudeMessage{
			{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "read /tmp/x"}}},
		},
		Tools: []ClaudeToolDef{
			{Name: "read", Description: "Read a file", InputSchema: map[string]interface{}{"type": "object"}},
		},
	}

	events, err := client.ClaudeChatStream(context.Background(), req)
	if err != nil {
		t.Fatalf("ClaudeChatStream() error: %v", err)
	}

	// Accumulate the same way chatOnce does.
	blockMap := make(map[int]*ClaudeContentBlock)
	var blocks []ClaudeContentBlock
	var stopReason string
	for ev := range events {
		if ev.Error != "" {
			t.Fatalf("stream error: %s", ev.Error)
		}
		switch ev.Type {
		case "content_block_start":
			if ev.Block != nil {
				blockMap[ev.Index] = ev.Block
			}
		case "content_block_delta":
			if b := blockMap[ev.Index]; b != nil && ev.Delta != nil && ev.Delta.Type == "input_json_delta" {
				b.InputJSON += ev.Delta.PartialJSON
			}
		case "content_block_stop":
			if b := blockMap[ev.Index]; b != nil {
				if b.Type == "tool_use" && b.InputJSON != "" {
					var input map[string]interface{}
					if err := json.Unmarshal([]byte(b.InputJSON), &input); err == nil {
						b.Input = input
					}
				}
				blocks = append(blocks, *b)
				delete(blockMap, ev.Index)
			}
		case "message_delta":
			stopReason = ev.StopReason
		}
	}

	resp := &ClaudeResponse{Content: blocks, StopReason: stopReason}
	if resp.StopReason != "tool_use" {
		t.Errorf("expected stop_reason tool_use, got %q", resp.StopReason)
	}
	calls := resp.ToolCalls()
	if len(calls) != 1 || calls[0].Name != "read" {
		t.Fatalf("expected 1 read tool call, got %+v", calls)
	}
	if calls[0].Arguments["path"] != "/tmp/x" {
		t.Errorf("expected path argument, got %+v", calls[0].Arguments)
	}
}

// TestClaudeChat_NonStreaming drives the non-streaming ClaudeChat path.
func TestClaudeChat_NonStreaming(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ClaudeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if req.Stream {
			t.Errorf("expected stream=false, got true")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":    "resp-1",
			"type":  "message",
			"role":  "assistant",
			"model": "deepseek-v4-flash",
			"content": []map[string]interface{}{
				{"type": "thinking", "thinking": "let me think", "signature": "sig_1"},
				{"type": "text", "text": "The answer is 42."},
			},
			"stop_reason": "end_turn",
			"usage":       map[string]interface{}{"input_tokens": 10, "output_tokens": 7},
		}) //stylegate:ignore encodes a stub response; a failure surfaces as a client-side decode error in the same test
	}))
	defer server.Close()

	client := &Client{
		BaseURL:     server.URL,
		APIKey:      "test-key",
		HTTPClient:  server.Client(),
		RetryConfig: DefaultRetryConfig(),
	}

	resp, err := client.ClaudeChat(context.Background(), &ClaudeRequest{
		Model:     "deepseek-v4-flash",
		MaxTokens: DefaultClaudeMaxTokens,
		Messages:  []ClaudeMessage{{Role: "user", Content: []ClaudeContentBlock{{Type: "text", Text: "hi"}}}},
	})
	if err != nil {
		t.Fatalf("ClaudeChat() error: %v", err)
	}

	if resp.Text() != "The answer is 42." {
		t.Errorf("Text() = %q", resp.Text())
	}
	thinking, sig := resp.Thinking()
	if thinking != "let me think" || sig != "sig_1" {
		t.Errorf("Thinking() = %q/%q", thinking, sig)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q", resp.StopReason)
	}
	if resp.Usage.OutputTokens != 7 || resp.Usage.InputTokens != 10 {
		t.Errorf("Usage = %+v", resp.Usage)
	}
}

// TestClaudeMessagesURL covers ANTHROPIC_BASE_URL with and without the
// "/anthropic" prefix, plus the messages_path override (settings.json
// "messages_path") that takes precedence and is appended verbatim.
func TestClaudeMessagesURL(t *testing.T) {
	cases := []struct {
		base         string
		messagesPath string
		want         string
	}{
		{"https://api.deepseek.com", "", "https://api.deepseek.com/anthropic/messages"},
		// Base ending in /anthropic (DeepSeek + GLM convention): the
		// standard Anthropic wire path /v1/messages is appended. DeepSeek
		// accepts /anthropic/v1/messages (HTTP 200, verified live); GLM
		// REQUIRES it — /anthropic/messages returns a 200 + "404 NOT_FOUND"
		// empty-body stream (the 0-token GLM bug, fixed 2026-08-27).
		{"https://api.deepseek.com/anthropic", "", "https://api.deepseek.com/anthropic/v1/messages"},
		{"https://api.deepseek.com/anthropic/", "", "https://api.deepseek.com/anthropic/v1/messages"},
		{"https://open.bigmodel.cn/api/anthropic", "", "https://open.bigmodel.cn/api/anthropic/v1/messages"},
		{"https://proxy.example.com/v1", "", "https://proxy.example.com/v1/anthropic/messages"},
		// messages_path override: full path appended verbatim to base —
		// Tencent hy4-preview (https://tokenhub.tencentmaas.com/v1/messages,
		// verified live 2026-08-28). Wins over BOTH legacy derivations,
		// including "/anthropic"-suffixed bases.
		{"https://tokenhub.tencentmaas.com", "/v1/messages", "https://tokenhub.tencentmaas.com/v1/messages"},
		{"https://tokenhub.tencentmaas.com/", "/v1/messages", "https://tokenhub.tencentmaas.com/v1/messages"},
		{"https://open.bigmodel.cn/api/anthropic", "/v1/messages", "https://open.bigmodel.cn/api/anthropic/v1/messages"},
		{"https://proxy.example.com", "/custom/messages", "https://proxy.example.com/custom/messages"},
	}
	for _, c := range cases {
		client := &Client{BaseURL: c.base, MessagesPath: c.messagesPath}
		if got := client.claudeMessagesURL(); got != c.want {
			t.Errorf("claudeMessagesURL(%q, messages_path=%q) = %q, want %q", c.base, c.messagesPath, got, c.want)
		}
	}
}

// TestClaudeUsageTotalTokens verifies the usage accumulator.
func TestClaudeUsageTotalTokens(t *testing.T) {
	u := ClaudeUsage{InputTokens: 10, OutputTokens: 5, CacheReadInputTokens: 100}
	if u.TotalTokens() != 115 {
		t.Errorf("TotalTokens() = %d, want 115", u.TotalTokens())
	}

	ct := &CostTracker{}
	ct.AddClaudeUsage(u)
	if ct.TotalPromptTokens != 110 || ct.TotalCompletionTokens != 5 || ct.TotalCacheHitTokens != 100 {
		t.Errorf("tracker after AddClaudeUsage: %+v", ct)
	}
}

// TestBuildClaudeRequest_CacheControl covers A5: system blocks and the LAST
// tool def carry the ephemeral cache_control marker (Anthropic standard),
// other tool defs do not, and the marker never leaks into the caller's
// (package-global) tool definitions — the request gets a copy.
func TestBuildClaudeRequest_CacheControl(t *testing.T) {
	tools := []ClaudeToolDef{
		{Name: "read", InputSchema: map[string]interface{}{"type": "object"}},
		{Name: "bash", InputSchema: map[string]interface{}{"type": "object"}},
	}
	req := BuildClaudeRequest("m", nil, "system prompt", "project ctx", tools, 8192, nil)

	// Last system block marked (Anthropic convention: one marker per prefix).
	if len(req.System) != 2 {
		t.Fatalf("expected 2 system blocks, got %d", len(req.System))
	}
	if req.System[0].CacheControl != nil {
		t.Error("first system block must not carry cache_control")
	}
	if req.System[1].CacheControl == nil || req.System[1].CacheControl.Type != "ephemeral" {
		t.Error("last system block must carry cache_control ephemeral")
	}

	// Only the last tool def marked.
	if len(req.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(req.Tools))
	}
	if req.Tools[0].CacheControl != nil {
		t.Error("non-last tool def must not carry cache_control")
	}
	if req.Tools[1].CacheControl == nil || req.Tools[1].CacheControl.Type != "ephemeral" {
		t.Error("last tool def must carry cache_control ephemeral")
	}

	// The caller's slice is untouched (it may be package-global state).
	if tools[0].CacheControl != nil || tools[1].CacheControl != nil {
		t.Error("cache_control leaked into the caller's tool definitions")
	}

	// No tools → no tools field, no marker, no panic.
	req2 := BuildClaudeRequest("m", nil, "", "", nil, 8192, nil)
	if len(req2.Tools) != 0 {
		t.Errorf("expected no tools, got %d", len(req2.Tools))
	}
	if len(req2.System) != 0 {
		t.Errorf("expected no system blocks, got %d", len(req2.System))
	}
}

// TestBuildClaudeRequest_CacheControlWireFormat verifies the marker
// serializes exactly as the Anthropic standard requires.
func TestBuildClaudeRequest_CacheControlWireFormat(t *testing.T) {
	req := BuildClaudeRequest("m", nil, "sys", "", []ClaudeToolDef{
		{Name: "read", InputSchema: map[string]interface{}{"type": "object"}},
	}, 100, nil)
	b, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	want := `"cache_control":{"type":"ephemeral"}`
	if !bytes.Contains(b, []byte(want)) {
		t.Errorf("wire JSON missing %s, got: %s", want, string(b))
	}
}

// TestDefaultClaudeMaxTokens_Converged pins the A4 convergence decision
// (2026-08-22): 384K → 16384. Measured live: the endpoint hard-enforces
// max_tokens; a 607-line single write draft consumed ~5,968 completion tokens
// (worst thinking+draft round ~7,500 of 8,192); 16,384 keeps 2x headroom over
// the measured worst case while bounding the per-response ceiling 23x below
// the model's 384K limit. See devdoc/maxtokens-converged.md.
func TestDefaultClaudeMaxTokens_Converged(t *testing.T) {
	if DefaultClaudeMaxTokens != 16384 {
		t.Errorf("DefaultClaudeMaxTokens = %d, want the A4-converged 16384", DefaultClaudeMaxTokens)
	}
	if DefaultClaudeMaxTokens >= DeepSeekV4MaxOutputTokens {
		t.Errorf("converged default %d must stay below the model ceiling %d", DefaultClaudeMaxTokens, DeepSeekV4MaxOutputTokens)
	}
}

// TestMessageImageRoundTrip verifies a user message carrying N images
// survives JSON serialization — required so resume checkpoints (which capture
// the raw request) keep the image bytes intact across a resume. It pins the
// PLURAL field: one message carries the whole turn's images.
func TestMessageImageRoundTrip(t *testing.T) {
	m := Message{Role: "user", Content: "look", Images: []ClaudeImageSource{
		{Type: "base64", MediaType: "image/png", Data: "aGk="},
		{Type: "base64", MediaType: "image/jpeg", Data: "dHdv"},
	}}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var back Message
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Images) != 2 {
		t.Fatalf("round-trip lost images: %d kept (raw %s)", len(back.Images), raw)
	}
	if back.Images[0].Data != "aGk=" || back.Images[0].MediaType != "image/png" {
		t.Fatalf("round-trip damaged image 0: %+v (raw %s)", back.Images[0], raw)
	}
	if back.Images[1].Data != "dHdv" || back.Images[1].MediaType != "image/jpeg" {
		t.Fatalf("round-trip damaged image 1: %+v (raw %s)", back.Images[1], raw)
	}
}
