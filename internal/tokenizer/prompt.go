package tokenizer

// PromptTokens counts a chat request the way the endpoint counts it, by
// rendering the request through DeepSeek's OFFICIAL V4.1 chat template and
// encoding the result — `apply_chat_template(..., tokenize=True)`, nothing
// added.
//
// # The template, and where it lives
//
// The rendering below is a line-by-line translation of
// `deepseek-v41-chat_template.jinja` (252 lines), fetched from the model repo
// over the searchbox channel and carried verbatim in the run's evidence. The
// previous version translated the V3 template from
// `~/Downloads/deepseek_v3_tokenizer/tokenizer_config.json`, whose tool-call
// grammar (`<｜tool▁call▁begin｜>function<｜tool▁sep｜>name\n```json…`) does not
// exist in V4.1: V4.1 writes `<｜DSML｜ invoke name="…">` and
// `<｜DSML｜ parameter name="…" string="true|false">`, renders the request's TOOL
// SCHEMAS in a preamble the endpoint counts, wraps a tool result in
// `<tool_result>…</tool_result>`, and closes every DSML element with its own
// tag. Two grammars, two counts.
//
// # Why this file produces SEGMENTS
//
// The endpoint tokenizes the rendered prompt as ONE string, but its added-token
// matching applies only to TEMPLATE text: measured against the live endpoint
// (`POST /anthropic/v1/messages/count_tokens`), `<｜User｜>` spelled inside
// MESSAGE CONTENT costs 4 tokens and `｜DSML｜` inside a parameter VALUE costs
// 4, while the same markers emitted by the template cost 1 each. So every
// rendered piece carries its provenance (`Segment.Template`), and the count is
// one encoding pass over the whole prompt with a byte mask: added-token
// matching is allowed only where the template emitted the bytes
// (Tokenizer.EncodeMasked). The pre-tokenizer and BPE still run across
// content/template boundaries, which is why a long parameter value ending in
// CJK punctuation followed by `</｜DSML｜ parameter>` counts exactly as the
// endpoint does — a per-segment sum loses that boundary merge.
//
// # The tokenizer
//
// The embedded tokenizer.json is the V4.1 file (1,283 added tokens). Its BPE
// vocabulary, merges, pre-tokenizer and normalizer are IDENTICAL to the V3
// file this package used to carry; only `added_tokens` grew. That growth is
// what adds `｜DSML｜` (id 128825) and `<｜System｜>`, the markers the V4.1
// template is built from. With the V3 data, `｜DSML｜` fell through to BPE and
// was charged four tokens where the endpoint charges one — the single largest
// source of the residual this file's previous version could not remove.
//
// # What it does not reproduce
//
// The endpoint is an Anthropic-compatible gateway in front of the model. On a
// request whose last message is an ASSISTANT turn, its count carries a
// generation header the template only emits after a user turn — measured +2
// tokens in chat mode with no tools and +1 in the other combinations (the
// header's spelling merges with the preceding EOS differently). gopi's own
// requests end on a user turn (the API requires it for generation) and every
// raw-log pair measured does too; this shape is reported by `--compare` rather
// than guessed into the count. On every other shape measured — 102 pairs from
// five independent raw logs, the 58,570-token benchmark request, and a matrix
// of minimal requests — the residual is zero.
//
// # Divergences from the template, each deliberate
//
// The template has five `raise_exception` sites. Four cannot be reached from
// the shapes gopi sends and are handled as follows, rather than by inventing an
// error path the callers do not have:
//
//   - image placeholder inside a text block: no image support here, so no check;
//   - invalid thinking_mode / invalid reasoning_effort: this renderer takes a
//     validated `thinking bool` and `budget int`, so there is nothing to reject;
//   - unknown role: rendered as a user turn (the template raises; gopi's caller
//     maps every role before it gets here);
//   - conflicting tool namespaces: the explicit namespace wins.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// Block is one element of a message's content array.
type Block struct {
	Type    string // BlockText, BlockToolUse, or BlockToolResult
	Text    string // Type == BlockText
	Name    string // Type == BlockToolUse: the tool's name
	ID      string // Type == BlockToolUse/ToolResult: the call id
	Input   string // Type == BlockToolUse: the arguments, already JSON-encoded
	Content string // Type == BlockToolResult: the result, already rendered to text
}

// Message is one entry of the request's message list. A message carries either
// Text (a plain string content) or Blocks (a content block array). Reasoning is
// the assistant's `thinking` content, which the template renders (in thinking
// mode) before the message text.
type Message struct {
	Role      string
	Text      string
	Reasoning string
	Blocks    []Block
}

// Block type discriminators, spelled once (Rule 1/13).
const (
	BlockText       = "text"
	BlockToolUse    = "tool_use"
	BlockToolResult = "tool_result"
)

// Roles this file branches on: the template's own three, plus the reminder role
// the template renders with its own marker.
const (
	RoleUser           = "user"
	RoleAssistant      = "assistant"
	RoleSystem         = "system"
	RoleTool           = "tool"
	RoleLatestReminder = "latest_reminder"
)

// PromptOptions carries the request-level switches the template reads.
//
// Thinking is `thinking_mode == "thinking"` — the endpoint's default when the
// request carries no `thinking` field, which is why the zero value is NOT the
// endpoint's default and callers must set it from the request. Effort is the
// numeric reasoning budget the template prints in its index-0 prefix; 0 means
// the template's default ("high", 75 in this template's map).
type PromptOptions struct {
	Thinking bool
	Effort   int
}

// DefaultEffort is the template's `reasoning_effort` default ("high" in this
// template's own map). Only the SPELLING of the number reaches the prompt, and
// every two- or three-digit budget below 100 is one token, so the exact value
// is not load-bearing for a count; it is carried so the rendered bytes are the
// template's.
const DefaultEffort = 75

// EffortBudget maps a request's `reasoning_effort` value to the template's
// budget. The template's own map is {"low": 50, "medium": 62, "high": 75,
// "max": 100}; an integer in [1, 100] is taken as-is.
func EffortBudget(v any) int {
	switch e := v.(type) {
	case string:
		switch e {
		case "low":
			return 50
		case "medium":
			return 62
		case "high":
			return 75
		case "max":
			return 100
		}
	case int:
		if e >= 1 && e <= 100 {
			return e
		}
	case float64:
		if e >= 1 && e <= 100 && e == float64(int(e)) {
			return int(e)
		}
	}
	return DefaultEffort
}

// The template's marker literals, taken verbatim from the .jinja source. They
// use the full-width vertical bar (U+FF5C) and U+2581 in place of a space, and
// each is a SINGLE added token in the V4.1 vocabulary.
const (
	markBOS            = "<｜begin▁of▁sentence｜>"
	markEOS            = "<｜end▁of▁sentence｜>"
	markUser           = "<｜User｜>"
	markAssistant      = "<｜Assistant｜>"
	markSystem         = "<｜System｜>"
	markLatestReminder = "<｜latest_reminder｜>"
	markThinkStart     = "<think>"
	markThinkEnd       = "</think>"
)

// dsml is the template's namespace prefix for tool traffic (`{%- set DSML =
// "｜DSML｜" -%}`), a single added token in the V4.1 vocabulary.
const dsml = "｜DSML｜"

// cacheControlKey is the request-side caching hint the endpoint's own tokenizer
// never sees (measured: identical server totals with and without it). It is
// named so the reshape below can drop it deliberately rather than by accident.
const cacheControlKey = "cache_control"

// The template's own literal text, spelled once.
const (
	effortTemplate = "Reasoning Effort: %d (range 1-100, the higher the value, the more thorough the reasoning)\n\n"
	toolsPreamble  = "## Tools\n\nYou have access to a set of tools to help answer the user's question. You can invoke tools by writing a \"<" + dsml + " calls>\" block like the following:\n\n<" + dsml + " calls>\n<" + dsml + " invoke name=\"$TOOL_NAME\">\n<" + dsml + " parameter name=\"$PARAMETER_NAME\" string=\"true|false\">$PARAMETER_VALUE</" + dsml + " parameter>\n...\n</" + dsml + " invoke>\n<" + dsml + " invoke name=\"$TOOL_NAME2\">\n...\n</" + dsml + " invoke>\n</" + dsml + " calls>\n\nString parameters should be specified as is and set `string=\"true\"`. For all other types (numbers, booleans, arrays, objects), pass the value in JSON format and set `string=\"false\"`.\n\nIf thinking_mode is enabled (triggered by " + markThinkStart + "), you MUST output your complete reasoning inside " + markThinkStart + "..." + markThinkEnd + " BEFORE any tool calls or final response.\n\nOtherwise, output directly after " + markThinkEnd + " with tool calls or final response.\n\n### Available Tool Schemas\n\n"
	toolsTail      = "\n\nYou MUST strictly follow the above defined tool name and parameter schemas to invoke tool calls.\n"
	responseFormat = "\n\n## Response Format:\n\nYou MUST strictly adhere to the following schema to reply:\n"
	callsOpen      = "\n\n<" + dsml + " calls>\n"
	invokeOpen     = "<" + dsml + " invoke name=\""
	invokeClose    = "\">\n"
	invokeTail     = "\n</" + dsml + " invoke>"
	callsTail      = "\n</" + dsml + " calls>"
	paramOpen      = "<" + dsml + " parameter name=\""
	paramStrTrue   = "\" string=\"true\">"
	paramStrFalse  = "\" string=\"false\">"
	paramTail      = "</" + dsml + " parameter>"
	blankLine      = "\n\n"
	newline        = "\n"
)

// Segment is one piece of a rendered prompt.
//
// Template is true when the text was emitted by the TEMPLATE itself: it is
// encoded with added-token matching on, so a marker such as `｜DSML｜` or
// `<｜end▁of▁sentence｜>` costs one token each. False means interpolated request
// content (message text, a parameter value, a tool schema): it is ordinary BPE,
// because that is how the endpoint charges text inside a message.
type Segment struct {
	Text     string
	Template bool
}

// CountTemplate returns the token count of a template literal, encoded with
// added-token matching.
func (t *Tokenizer) CountTemplate(text string) int { return len(t.Encode(text)) }

// CountText returns the token count of text encoded WITHOUT added-token
// matching — what the endpoint charges for interpolated request content.
func (t *Tokenizer) CountText(text string) int {
	if text == "" {
		return 0
	}
	var ids []uint32
	var pieces []string
	pieces = t.pre.splitPre(text, pieces)
	return len(t.encodePieces(ids, pieces))
}

// PromptTokens returns the token count of a rendered chat request in chat mode.
// Callers counting a real request must use PromptTokensWith and pass the
// request's own options.
func (t *Tokenizer) PromptTokens(system string, messages []Message, toolSchemas []json.RawMessage) (int, error) {
	return t.PromptTokensWith(system, messages, toolSchemas, PromptOptions{})
}

// PromptTokensWith returns the token count of a rendered chat request.
//
// The prompt is assembled back into ONE string and encoded in one pass, with
// added-token matching allowed only at template positions. Counting segment by
// segment loses the pre-tokenizer/BPE merges that cross a content/template
// boundary; measured against the endpoint, that split model overshoots a long
// parameter value by a token per boundary.
func (t *Tokenizer) PromptTokensWith(system string, messages []Message, toolSchemas []json.RawMessage, opts PromptOptions) (int, error) {
	segs := RenderSegmentsWith(system, messages, toolSchemas, opts)
	var b strings.Builder
	var mask []bool
	for _, s := range segs {
		b.WriteString(s.Text)
		for i := 0; i < len(s.Text); i++ {
			mask = append(mask, s.Template)
		}
	}
	ids, err := t.EncodeMasked(b.String(), mask)
	if err != nil {
		// The mask is built from the segments one byte at a time, so its length
		// always equals the rendered string's; the error is propagated rather
		// than asserted away so a future segment source that breaks that
		// invariant surfaces as a count a caller must handle, never as a panic
		// (PROJECT.md Rule 19) and never as a silent zero (Rule 15).
		return 0, fmt.Errorf("tokenizer: counting the rendered prompt: %w", err)
	}
	return len(ids), nil
}

// RenderPrompt returns the rendered prompt as ONE string — the bytes the
// endpoint tokenizes, for inspection and for byte-level assertions. The count
// must go through RenderSegmentsWith, never through Encode of this string: the
// template/content split is what the endpoint charges.
func RenderPrompt(system string, messages []Message, toolSchemas []json.RawMessage) string {
	return RenderPromptWith(system, messages, toolSchemas, PromptOptions{})
}

// RenderPromptWith returns the rendered prompt as one string, in the given mode.
func RenderPromptWith(system string, messages []Message, toolSchemas []json.RawMessage, opts PromptOptions) string {
	var out []byte
	for _, s := range RenderSegmentsWith(system, messages, toolSchemas, opts) {
		out = append(out, s.Text...)
	}
	return string(out)
}

// RenderSegments renders the request exactly as the template does, in chat
// mode. See RenderSegmentsWith.
func RenderSegments(system string, messages []Message, toolSchemas []json.RawMessage) []Segment {
	return RenderSegmentsWith(system, messages, toolSchemas, PromptOptions{})
}

// RenderSegmentsWith renders the request exactly as the template does, in
// segments tagged by provenance.
//
// The three passes below are the template's own numbered steps, in its order:
// merge tool results and consecutive user turns, attach the top-level tool list
// to a leading system message, reorder tool results against the preceding
// assistant's call ids, then render.
func RenderSegmentsWith(system string, messages []Message, toolSchemas []json.RawMessage, opts PromptOptions) []Segment {
	msgs := templateMessages(system, messages, toolSchemas)
	segs := make([]Segment, 0, 4*len(msgs)+4)
	tmpl := func(s string) {
		if s != "" {
			segs = append(segs, Segment{Text: s, Template: true})
		}
	}
	content := func(s string) {
		if s != "" {
			segs = append(segs, Segment{Text: s})
		}
	}

	thinking := opts.Thinking
	budget := opts.Effort
	if budget == 0 {
		budget = DefaultEffort
	}

	// The template's own "effective_drop": tools anywhere disable drop_thinking.
	effectiveDrop := true
	for _, m := range msgs {
		if m.toolsSet {
			effectiveDrop = false
			break
		}
	}
	lastUser := -1
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].role == RoleUser || (msgs[i].role == RoleSystem && i > 0) {
			lastUser = i
			break
		}
	}

	tmpl(markBOS)

	for idx, m := range msgs {
		if idx == 0 {
			if thinking || m.role == RoleSystem {
				tmpl(markSystem)
			}
			if thinking {
				tmpl(fmt.Sprintf(effortTemplate, budget))
			}
		}
		switch m.role {
		case RoleSystem:
			if idx > 0 {
				tmpl(markSystem)
			}
			content(m.text)
			if m.toolsSet && len(m.tools) > 0 {
				tmpl(blankLine)
				renderTools(tmpl, content, m.tools)
			}
			if m.responseFormat {
				tmpl(responseFormat)
				content(tojson(m.responseSchema))
			}

		case RoleUser:
			tmpl(markUser)
			// The template joins the parts with a blank line AFTER wrapping a
			// tool result; the joined body is ONE text run, and the endpoint
			// tokenizes it as one (measured: five text blocks counted part by
			// part overshoot by four tokens on a 19.5 KB message).
			content(joinBlocks(m.blocks))

		case RoleLatestReminder:
			tmpl(markLatestReminder)
			content(m.text)

		case RoleAssistant:
			if thinking && !m.prevHasTask && (!effectiveDrop || idx > lastUser) {
				content(m.reasoning)
				tmpl(markThinkEnd)
			}
			content(m.text)
			if len(m.calls) > 0 {
				renderCalls(tmpl, content, m.calls)
			}
			if !m.woEOS {
				tmpl(markEOS)
			}
		}

		// Transition markers: only when the NEXT turn is an assistant/reminder
		// turn, or this is the last message (the endpoint counts with
		// add_generation_prompt, so the last message gets its header too).
		last := idx == len(msgs)-1
		nextAssistant := !last && (msgs[idx+1].role == RoleAssistant || msgs[idx+1].role == RoleLatestReminder)
		if last || nextAssistant {
			if m.role == RoleUser || (m.role == RoleSystem && idx > 0) {
				tmpl(markAssistant)
				if !effectiveDrop && thinking {
					tmpl(markThinkStart)
				} else if effectiveDrop && thinking && idx >= lastUser {
					tmpl(markThinkStart)
				} else {
					tmpl(markThinkEnd)
				}
			}
		}
	}
	return coalesce(segs)
}

// coalesce merges ADJACENT segments of the same provenance.
//
// This is not cosmetic: the endpoint encodes a contiguous literal run as ONE
// string, and tokenization merges across what a structural split would make
// separate strings. Measured on a one-call exchange, counting the six template
// pieces around the call name separately charges 22 tokens where the endpoint
// charges 20 — the two newlines at a piece boundary are one token there and two
// here. Content runs are merged for the same reason (five text blocks counted
// part by part overshot a 19.5 KB message by four tokens).
func coalesce(segs []Segment) []Segment {
	if len(segs) < 2 {
		return segs
	}
	out := segs[:1]
	for _, s := range segs[1:] {
		last := &out[len(out)-1]
		if last.Template == s.Template {
			last.Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

// tmsg is one message after the template's three preparation passes: the shape
// its rendering branches read.
type tmsg struct {
	role           string
	text           string
	reasoning      string
	blocks         []Block
	calls          []Block
	prevHasTask    bool
	woEOS          bool
	tools          []json.RawMessage
	toolsSet       bool
	responseFormat bool
	responseSchema any
}

// templateMessages performs the template's steps 1–3: merge tool results and
// consecutive user turns, attach the top-level tool list, reorder tool results
// against the preceding assistant's call order.
func templateMessages(system string, messages []Message, toolSchemas []json.RawMessage) []tmsg {
	var out []tmsg

	if system != "" {
		out = append(out, tmsg{role: RoleSystem, text: system})
	}
	for _, m := range messages {
		switch m.Role {
		case RoleTool:
			// A tool result in its own message: the template turns it into a
			// tool_result block on the preceding user turn (or on a new one).
			blk := Block{Type: BlockToolResult, Content: m.Text}
			if n := len(out); n > 0 && out[n-1].role == RoleUser {
				out[n-1].blocks = append(out[n-1].blocks, blk)
				continue
			}
			out = append(out, tmsg{role: RoleUser, blocks: []Block{blk}})

		case RoleUser:
			blocks := m.Blocks
			if len(blocks) == 0 {
				blocks = []Block{{Type: BlockText, Text: m.Text}}
			}
			if n := len(out); n > 0 && out[n-1].role == RoleUser {
				out[n-1].blocks = append(out[n-1].blocks, blocks...)
				continue
			}
			out = append(out, tmsg{role: RoleUser, blocks: blocks})

		case RoleAssistant:
			t := tmsg{role: RoleAssistant, text: m.Text, reasoning: m.Reasoning}
			for _, b := range m.Blocks {
				switch b.Type {
				case BlockToolUse:
					t.calls = append(t.calls, b)
				case BlockText:
					t.text += b.Text
				}
			}
			out = append(out, t)
		}
	}

	// Step: the top-level tool list attaches to a leading system message, or to
	// a NEW empty one — which is why a request with tools and no system prompt
	// still renders a system marker.
	if len(toolSchemas) > 0 {
		if len(out) > 0 && out[0].role == RoleSystem {
			if !out[0].toolsSet {
				out[0].tools = toolSchemas
				out[0].toolsSet = true
			}
		} else {
			out = append([]tmsg{{role: RoleSystem, tools: toolSchemas, toolsSet: true}}, out...)
		}
	}

	// Step: reorder a user turn's tool_result blocks to the order of the
	// preceding assistant's tool_calls ids. Only when there is more than one.
	for i := range out {
		if out[i].role == RoleAssistant && len(out[i].calls) > 0 {
			ids := make([]string, 0, len(out[i].calls))
			for _, c := range out[i].calls {
				ids = append(ids, c.ID)
			}
			if i+1 < len(out) && out[i+1].role == RoleUser {
				out[i+1].blocks = orderResults(out[i+1].blocks, ids)
			}
		}
	}
	return out
}

// orderResults sorts a user turn's tool_result blocks by their position in the
// assistant's call ids; blocks with no match keep order 0, exactly as the
// template's `order` namespace defaults it.
func orderResults(blocks []Block, ids []string) []Block {
	results := 0
	for _, b := range blocks {
		if b.Type == BlockToolResult {
			results++
		}
	}
	if results < 2 {
		return blocks
	}
	order := func(b Block) int {
		for i, id := range ids {
			if id != "" && id == b.ID {
				return i
			}
		}
		return 0
	}
	sorted := make([]Block, 0, results)
	for _, b := range blocks {
		if b.Type == BlockToolResult {
			sorted = append(sorted, b)
		}
	}
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && order(sorted[j]) < order(sorted[j-1]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}
	next := 0
	out := make([]Block, len(blocks))
	for i, b := range blocks {
		if b.Type != BlockToolResult {
			out[i] = b
			continue
		}
		out[i] = sorted[next]
		next++
	}
	return out
}

// joinBlocks renders a user turn's content blocks the way the template does:
// text parts and `<tool_result>`-wrapped results, joined by a blank line —
// `up.parts | join("\n\n")`.
func joinBlocks(blocks []Block) string {
	var parts []string
	for _, b := range blocks {
		switch b.Type {
		case BlockText:
			parts = append(parts, b.Text)
		case BlockToolResult:
			parts = append(parts, "<tool_result>"+b.Content+"</tool_result>")
		case "":
			// A block without a type is what the caller meant as text.
			parts = append(parts, b.Text)
		default:
			parts = append(parts, "[Unsupported "+b.Type+"]")
		}
	}
	return strings.Join(parts, blankLine)
}

// renderCalls renders an assistant message's tool_calls block, which V4.1
// spells with the DSML namespace:
//
//	\n\n<｜DSML｜ calls>\n<｜DSML｜ invoke name="N">\n<｜DSML｜ parameter name="k" string="true">v</｜DSML｜ parameter>\n</｜DSML｜ invoke>\n</｜DSML｜ calls>
//
// A parameter whose value is a string gets `string="true"` and its raw text; any
// other value gets `string="false"` and its JSON.
func renderCalls(tmpl func(string), content func(string), calls []Block) {
	tmpl(callsOpen)
	for ci, c := range calls {
		if ci > 0 {
			tmpl(newline)
		}
		tmpl(invokeOpen)
		content(qualifiedName(c.Name))
		tmpl(invokeClose)
		args := callArgs(c.Input)
		for pi, kv := range args {
			if pi > 0 {
				tmpl(newline)
			}
			tmpl(paramOpen)
			content(kv.key)
			if kv.isString {
				tmpl(paramStrTrue)
				content(kv.value)
			} else {
				tmpl(paramStrFalse)
				content(kv.value)
			}
			tmpl(paramTail)
		}
		tmpl(invokeTail)
	}
	tmpl(callsTail)
}

// arg is one key/value pair of a tool call's arguments, in the order the
// arguments object holds them.
type arg struct {
	key      string
	value    string
	isString bool
}

// callArgs decodes a tool call's arguments JSON into ordered pairs. An argument
// payload that is not a JSON object is treated as the template's own fallback:
// `{"arguments": <raw>}`, one parameter named `arguments`.
func callArgs(input string) []arg {
	if input == "" {
		return nil
	}
	v, err := parseOrderedValue([]byte(input))
	if err != nil || v.kind != 'o' {
		return []arg{{key: "arguments", value: input, isString: true}}
	}
	out := make([]arg, 0, len(v.obj))
	for _, f := range v.obj {
		if f.val.kind == 's' {
			out = append(out, arg{key: f.key, value: f.val.str, isString: true})
			continue
		}
		out = append(out, arg{key: f.key, value: f.val.JSON(), isString: false})
	}
	return out
}

// qualifiedName applies the template's `qualified_name`: a "ns::name" splits
// into a namespace and a name, and the name is what is rendered.
func qualifiedName(name string) string {
	for i := 0; i+1 < len(name); i++ {
		if name[i] == ':' && name[i+1] == ':' {
			return name[i+2:]
		}
	}
	return name
}

// renderTools renders the request's tool definitions the way the template's
// `render_tools` macro does: the fixed preamble, then each schema, joined by a
// newline, then the closing sentence.
//
// The gateway converts an Anthropic tool (`name`, `description`, `input_schema`)
// into the OpenAI function shape the template reads: `input_schema` is renamed
// `parameters`, Anthropic-only keys (`cache_control`) are dropped, and the
// object is re-serialized with json.dumps' own spacing and UTF-8 output.
// Measured on this request's 17 tools: every schema matches the endpoint
// token-for-token under that shape; leaving `input_schema` in place, sorting
// keys, or keeping `cache_control` each moves the count.
func renderTools(tmpl func(string), content func(string), schemas []json.RawMessage) {
	tmpl(toolsPreamble)
	// The schemas AND the closing sentence are ONE interpolated run: the
	// endpoint tokenizes them together (measured on a one-tool request: the
	// sentence counted as its own template segment overshoots by one token at
	// the schema/`\n\nYou MUST` boundary).
	var body strings.Builder
	for i, one := range schemas {
		if i > 0 {
			body.WriteString(newline)
		}
		body.WriteString(tojson(toolFunction(one)))
	}
	body.WriteString(toolsTail)
	content(body.String())
}

// toolFunction reshapes one Anthropic tool into the OpenAI function object the
// template renders, preserving member order.
func toolFunction(raw json.RawMessage) any {
	v, err := parseOrderedValue(raw)
	if err != nil || v.kind != 'o' {
		return jsonRawValue{v}
	}
	out := jval{kind: 'o'}
	for _, f := range v.obj {
		switch f.key {
		case "name":
			out.obj = append(out.obj, jfield{key: "name", val: f.val})
		case "description":
			out.obj = append(out.obj, jfield{key: "description", val: f.val})
		case "input_schema":
			out.obj = append(out.obj, jfield{key: "parameters", val: f.val})
		case cacheControlKey:
			// A CACHING HINT, not part of the tool definition: the endpoint
			// counts the definition only (measured: identical server totals
			// with and without the hint; charging it locally cost 13 tokens).
		}
	}
	return out
}

// jsonRawValue carries a value that could not be reshaped; it renders as the
// template's fallback would, as the value itself.
type jsonRawValue struct{ v jval }

// tojson renders a value the way the template's `tojson` filter does on the
// measured server: keys in the order they were parsed, UTF-8 output, no HTML
// escaping, and `", "` / `": "` separators.
func tojson(v any) string {
	switch x := v.(type) {
	case jval:
		return x.JSON()
	case jsonRawValue:
		if x.v.kind == 'o' && len(x.v.obj) > 0 {
			return x.v.JSON()
		}
		return x.v.JSON()
	case json.RawMessage:
		val, err := parseOrderedValue(x)
		if err != nil {
			return "null"
		}
		return val.JSON()
	default:
		val, err := parseOrderedValue(mustMarshal(v))
		if err != nil {
			return "null"
		}
		return val.JSON()
	}
}

// mustMarshal is the fallback encoder for values not already parsed.
func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

// ---------------------------------------------------------------------------
// Ordered JSON
//
// Python's json.dumps preserves the insertion order of the object it is given;
// Go's encoding/json sorts map keys. The endpoint's schema and argument text is
// the Python shape (measured), so this package parses to an order-preserving
// value and re-emits it with json.dumps' separators.

type jfield struct {
	key string
	val jval
}

// jval is one JSON value with object member order preserved.
type jval struct {
	kind byte // 'o' object, 'a' array, 's' string, 'n' number, 'b' bool, 'z' null
	obj  []jfield
	arr  []jval
	str  string
	num  string
	b    bool
}

// parseOrderedValue decodes one JSON value preserving object member order and
// number spelling.
func parseOrderedValue(raw []byte) (jval, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	v, err := parseOrderedFrom(dec)
	if err != nil {
		return jval{}, err
	}
	return v, nil
}

func parseOrderedFrom(dec *json.Decoder) (jval, error) {
	tok, err := dec.Token()
	if err != nil {
		return jval{}, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			out := jval{kind: 'o'}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return jval{}, err
				}
				key, ok := kt.(string)
				if !ok {
					return jval{}, fmt.Errorf("object key is not a string")
				}
				val, err := parseOrderedFrom(dec)
				if err != nil {
					return jval{}, err
				}
				out.obj = append(out.obj, jfield{key: key, val: val})
			}
			if _, err := dec.Token(); err != nil {
				return jval{}, err
			}
			return out, nil
		case '[':
			out := jval{kind: 'a'}
			for dec.More() {
				val, err := parseOrderedFrom(dec)
				if err != nil {
					return jval{}, err
				}
				out.arr = append(out.arr, val)
			}
			if _, err := dec.Token(); err != nil {
				return jval{}, err
			}
			return out, nil
		}
		return jval{}, fmt.Errorf("unexpected delimiter %v", t)
	case string:
		return jval{kind: 's', str: t}, nil
	case json.Number:
		return jval{kind: 'n', num: t.String()}, nil
	case bool:
		return jval{kind: 'b', b: t}, nil
	case nil:
		return jval{kind: 'z'}, nil
	}
	return jval{}, fmt.Errorf("unexpected JSON token %v", tok)
}

// JSON renders the value with json.dumps' default spacing and UTF-8 output.
func (v jval) JSON() string {
	var b strings.Builder
	v.write(&b)
	return b.String()
}

func (v jval) write(b *strings.Builder) {
	switch v.kind {
	case 'o':
		b.WriteByte('{')
		for i, f := range v.obj {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(quoteJSON(f.key))
			b.WriteString(": ")
			f.val.write(b)
		}
		b.WriteByte('}')
	case 'a':
		b.WriteByte('[')
		for i, e := range v.arr {
			if i > 0 {
				b.WriteString(", ")
			}
			e.write(b)
		}
		b.WriteByte(']')
	case 's':
		b.WriteString(quoteJSON(v.str))
	case 'n':
		b.WriteString(v.num)
	case 'b':
		if v.b {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case 'z':
		b.WriteString("null")
	}
}

// quoteJSON quotes a string the way Python's json.dumps(ensure_ascii=False)
// does: quote/backslash and control characters escaped, non-ASCII emitted as
// UTF-8.
func quoteJSON(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 2)
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				b.WriteString(`\u`)
				const hex = "0123456789abcdef"
				b.WriteByte('0')
				b.WriteByte('0')
				b.WriteByte(hex[(r>>4)&0xf])
				b.WriteByte(hex[r&0xf])
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
