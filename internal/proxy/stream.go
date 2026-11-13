package proxy

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"
)

// Anthropic SSE event frame written on the wire:
//
//	event: <type>
//	data: <json>
//
// The Anthropic Messages SSE stream follows this event order, reproduced by
// the translator below:
//
//	message_start
//	content_block_start   (per content block)
//	content_block_delta   (repeated per block)
//	content_block_stop    (per content block)
//	message_delta         (stop_reason + usage)
//	message_stop
const (
	eventMessageStart      = "message_start"
	eventContentBlockStart = "content_block_start"
	eventContentBlockDelta = "content_block_delta"
	eventContentBlockStop  = "content_block_stop"
	eventMessageDelta      = "message_delta"
	eventMessageStop       = "message_stop"
	eventError             = "error"
)

// AnthropicEvent is one SSE frame's JSON payload.
type AnthropicEvent struct {
	Type string `json:"type"`

	// message_start
	Message *AnthropicMessage `json:"message,omitempty"`

	// content_block_start / content_block_delta / content_block_stop
	Index        *int          `json:"index,omitempty"`
	ContentBlock *ContentBlock `json:"content_block,omitempty"`

	// delta object — used by BOTH content_block_delta (ContentBlockDelta) and
	// message_delta (AnthropicMessageDelta). Mutually exclusive in practise.
	Delta any `json:"delta,omitempty"`

	// message_start / message_delta
	Usage *AnthropicUsage `json:"usage,omitempty"`

	// error
	Error *AnthropicError `json:"error,omitempty"`
}

// AnthropicMessage is the message object carried by message_start.
type AnthropicMessage struct {
	ID         string          `json:"id"`
	Type       string          `json:"type"`
	Role       string          `json:"role"`
	Model      string          `json:"model"`
	Content    []ContentBlock  `json:"content"`
	StopReason *string         `json:"stop_reason,omitempty"`
	Usage      *AnthropicUsage `json:"usage,omitempty"`
}

// ContentBlockDelta is the delta object of a content_block_delta event.
type ContentBlockDelta struct {
	Type        string `json:"type"` // text_delta | thinking_delta | input_json_delta
	Text        string `json:"text,omitempty"`
	Thinking    string `json:"thinking,omitempty"`
	PartialJSON string `json:"partial_json,omitempty"`
	Signature   string `json:"signature,omitempty"`
}

// AnthropicMessageDelta is the delta object of a message_delta event.
type AnthropicMessageDelta struct {
	StopReason   string  `json:"stop_reason"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// AnthropicUsage carries token counts.
type AnthropicUsage struct {
	InputTokens          int `json:"input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	CacheReadInputTokens int `json:"cache_read_input_tokens,omitempty"`
}

// AnthropicError is the error envelope (also used for error events).
type AnthropicError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func intPtr(i int) *int { return &i }

// writeEvent writes one Anthropic SSE frame: "event: <type>\ndata: <json>\n\n".
func writeEvent(w io.Writer, ev AnthropicEvent) error {
	payload, _ := json.Marshal(ev) //stylegate:ignore marshalling a value built in-process cannot fail (no channels/funcs reach these sites)
	_, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, payload)
	return err
}

// writeErrorEvent writes an Anthropic error event (stream-error path).
func writeErrorEvent(w io.Writer, msg string) error {
	return writeEvent(w, AnthropicEvent{
		Type:  eventError,
		Error: &AnthropicError{Type: "api_error", Message: msg},
	})
}

// streamTranslator converts OpenAI chat.completion.chunk frames into Anthropic
// SSE events. NOT safe for concurrent use — one translator per request stream.
//
// Tool calls are the tricky part: OpenAI streams them as incremental deltas.
// The first fragment on a given index carries {id,name} (possibly only one of
// the two), and later fragments carry only {"arguments":"<frag>"} which must be
// concatenated in arrival order to recover the final JSON arguments string.
// Each tool-call index becomes a tool_use content block whose input_json_delta
// events carry the fragments verbatim. reasoning_content deltas map to Anthropic
// thinking blocks; ordinary content maps to text blocks.
type streamTranslator struct {
	msgID string
	model string

	// open block bookkeeping
	openType  string // "text" | "thinking" | "tool_use" | ""
	openIndex int

	// next content-block wire index (monotonic across the whole stream)
	blockSeq int

	// per-turn accumulation
	text     strings.Builder
	thinking strings.Builder
	// tool accumulation keyed by OpenAI delta.index
	tools map[int]*accTool
	// wire content-block index of each streaming tool block
	toolWireIdx map[int]int
	anyToolSeen bool
	started     bool
}

// accTool accumulates tool_use fragments for one OpenAI tool-call index.
type accTool struct {
	id        string
	name      string
	arguments strings.Builder
}

func newStreamTranslator(msgID, model string) *streamTranslator {
	return &streamTranslator{
		msgID:       msgID,
		model:       model,
		tools:       map[int]*accTool{},
		toolWireIdx: map[int]int{},
	}
}

// start emits message_start. Idempotent — safe to call multiple times.
func (t *streamTranslator) start(w io.Writer) error {
	if t.started {
		return nil
	}
	if t.msgID == "" {
		t.msgID = "msg_" + fmt.Sprintf("%d", time.Now().UnixNano())
	}
	if t.model == "" {
		t.model = "upstream"
	}
	msg := &AnthropicMessage{
		ID:      t.msgID,
		Type:    "message",
		Role:    "assistant",
		Model:   t.model,
		Content: []ContentBlock{},
	}
	t.started = true
	return writeEvent(w, AnthropicEvent{Type: eventMessageStart, Message: msg})
}

// closeBlock emits content_block_stop for the open block, if any.
func (t *streamTranslator) closeBlock(w io.Writer) error {
	if t.openType == "" {
		return nil
	}
	idx := t.openIndex
	t.openType = ""
	return writeEvent(w, AnthropicEvent{Type: eventContentBlockStop, Index: intPtr(idx)})
}

// beginBlock closes any open block and opens a fresh one, returning its index.
func (t *streamTranslator) beginBlock(w io.Writer, block ContentBlock) (int, error) {
	if err := t.closeBlock(w); err != nil {
		return 0, err
	}
	idx := t.blockSeq
	t.blockSeq++
	t.openType = block.Type
	t.openIndex = idx
	err := writeEvent(w, AnthropicEvent{Type: eventContentBlockStart, Index: intPtr(idx), ContentBlock: &block})
	return idx, err
}

// Feed processes one chunk and writes any resulting Anthropic events.
// message_start is emitted lazily on the first chunk (which carries the real
// upstream id/model).
func (t *streamTranslator) Feed(w io.Writer, chunk OpenAIChatChunk) error {
	if len(chunk.Choices) == 0 {
		return nil
	}
	if chunk.ID != "" {
		t.msgID = chunk.ID
	}
	if chunk.Model != "" {
		t.model = chunk.Model
	}
	if !t.started {
		if err := t.start(w); err != nil {
			return err
		}
	}
	ch := chunk.Choices[0]
	d := ch.Delta

	// 1. reasoning (thinking) delta.
	if d.ReasoningContent != nil && *d.ReasoningContent != "" {
		frag := *d.ReasoningContent
		if t.openType != "thinking" {
			if _, err := t.beginBlock(w, ContentBlock{Type: "thinking"}); err != nil {
				return err
			}
		}
		t.thinking.WriteString(frag)
		if err := writeEvent(w, AnthropicEvent{
			Type:  eventContentBlockDelta,
			Index: intPtr(t.openIndex),
			Delta: &ContentBlockDelta{Type: "thinking_delta", Thinking: frag},
		}); err != nil {
			return err
		}
	}

	// 2. tool-call deltas: open a tool_use block on first appearance and
	//    stream input_json_delta fragments as they arrive.
	if len(d.ToolCalls) > 0 {
		for _, tc := range d.ToolCalls {
			t.anyToolSeen = true
			acc, ok := t.tools[tc.Index]
			if !ok {
				acc = &accTool{
					id:   tc.ID,
					name: tc.Function.Name,
				}
				t.tools[tc.Index] = acc
				var id, name string
				if tc.ID != "" {
					id = tc.ID
				}
				if tc.Function.Name != "" {
					name = tc.Function.Name
				}
				// Open the tool_use block now. beginBlock closes any previously
				// open block (text/thinking/another tool), so a new tool index
				// correctly breaks the previous one. id/name come from the first
				// delta fragment, so we can stamp them on the start event.
				wire, err := t.beginBlock(w, ContentBlock{Type: "tool_use", ID: id, Name: name})
				if err != nil {
					return err
				}
				t.toolWireIdx[tc.Index] = wire
			}
			if tc.ID != "" {
				acc.id = tc.ID
			}
			if tc.Function.Name != "" {
				acc.name = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				acc.arguments.WriteString(tc.Function.Arguments)
				wire := t.toolWireIdx[tc.Index]
				if err := writeEvent(w, AnthropicEvent{
					Type:  eventContentBlockDelta,
					Index: intPtr(wire),
					Delta: &ContentBlockDelta{Type: "input_json_delta", PartialJSON: tc.Function.Arguments},
				}); err != nil {
					return err
				}
			}
		}
	}

	// 3. text (content) delta.
	if d.Content != nil && *d.Content != "" {
		frag := *d.Content
		if t.openType != "text" {
			if _, err := t.beginBlock(w, ContentBlock{Type: "text"}); err != nil {
				return err
			}
		}
		t.text.WriteString(frag)
		if err := writeEvent(w, AnthropicEvent{
			Type:  eventContentBlockDelta,
			Index: intPtr(t.openIndex),
			Delta: &ContentBlockDelta{Type: "text_delta", Text: frag},
		}); err != nil {
			return err
		}
	}

	// 4. finish.
	if ch.FinishReason != nil {
		return t.Finish(w, *ch.FinishReason, chunk.Usage)
	}
	return nil
}

// Finish closes the open block, emits message_delta with stop_reason + usage,
// then message_stop.
func (t *streamTranslator) Finish(w io.Writer, finishReason string, usage *OpenAIUsage) error {
	if err := t.closeBlock(w); err != nil {
		return err
	}
	stopReason := mapFinishReason(finishReason)
	ev := AnthropicEvent{
		Type:  eventMessageDelta,
		Delta: &AnthropicMessageDelta{StopReason: stopReason},
	}
	if usage != nil {
		au := AnthropicUsage{InputTokens: usage.PromptTokens, OutputTokens: usage.CompletionTokens}
		if usage.PromptTokensDetails != nil {
			au.CacheReadInputTokens = usage.PromptTokensDetails.CachedTokens
		}
		ev.Usage = &au
	}
	if err := writeEvent(w, ev); err != nil {
		return err
	}
	return writeEvent(w, AnthropicEvent{Type: eventMessageStop})
}

// mapFinishReason converts an OpenAI finish_reason into the Anthropic
// stop_reason the proxy emits on message_delta.
func mapFinishReason(fr string) string {
	switch fr {
	case "stop":
		return "end_turn"
	case "tool_calls", "function_call":
		return "tool_use"
	case "length":
		return "max_tokens"
	default:
		return "end_turn"
	}
}
