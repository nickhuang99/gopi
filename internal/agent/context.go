package agent

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/deepseek"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// DefaultContextWindow is the default token budget for conversation history.
// The deployment window is 200000: DeepSeek V4 advertises a 1M-token context,
// but prefill cost is the binding constraint — a 290K-token prefill measured
// ~16s (internal/deepseek/client.go), so single tasks run in a ~200K
// effective window. The original default was 900000 (1M minus completion
// headroom); under it, trimming kicked in at 540K (0.6 × 900K) — far too
// late for interactive round times.
const DefaultContextWindow = 200000

// DefaultCompactTriggerRatio is the fraction of the context window at which the
// ONE compression runs (context_compress.go). Below it the history is
// append-only; at it, the oldest run of messages is deleted once and the run's
// [IMPORTANT] notes are carried back to the head.
//
// 0.75, not 0.9 or 0.6: at 0.9 the history grew to ~810K tokens before
// anything was trimmed, and every late-task round re-prefilled a 200-400K
// token tail — measured ~16s for a 290K-token prefill (internal/deepseek/
// client.go), 15-30s/round late in long tasks (R1,
// devdoc/perf-vs-claude-fix-plan.md). 0.6 kept the cache prefix stable but
// fired too early at the 200K deployment window; ~75% matches the industry
// consensus (Claude Code et al., INDUSTRY-CONTEXT-MGMT.md §1).
const DefaultCompactTriggerRatio = 0.75

// DefaultCompactKeepRecent is how many of the most recent messages are never
// removed by compaction: the model is still working from the recent tail, and a
// compression that cut into it would break the round it is in the middle of.
//
// WHAT IT COUNTS IS SUBSTANTIVE MESSAGES, NOT SLOTS (fixed 2026-11-21). It used
// to count MESSAGES, and that made the floor a lottery on what the last eight
// array entries happened to be: a harness-injected `[system] …` checkpoint or a
// content-less assistant turn spends a slot without carrying anything the model
// can work from. Measured on one real session (task 01a1fae1-411a), the eight
// newest messages held 19 characters of real content beside 460 characters of
// harness injection — the floor was nominally intact and had protected almost
// nothing. The count now runs over the substantive messages that survive, and a
// TOKEN CEILING (compactKeepRecentTokensCap) bounds the other direction: eight
// substantive messages that are each 40 KB of tool output are not a tail anyone
// benefits from keeping verbatim.
const DefaultCompactKeepRecent = 8

// compactKeepRecentTokenCapDivisor is the fraction of DefaultContextWindow the
// recent tail may occupy (1/4). Named because the token ceiling's arithmetic
// must not spell a bare literal (Rule 13 — the gate that caught this change's
// first draft as `DefaultContextWindow / 4`).
const compactKeepRecentTokenCapDivisor = 4

// compactKeepRecentTokensCap is the TOKEN CEILING on the recent tail. The
// substantive count above says "keep at least DefaultCompactKeepRecent messages
// a reader would recognise as content"; this says "but do not hold more of the
// window hostage than this to do it". Without the ceiling, changing the floor's
// unit from slots to substantive messages would let a tail of eight 40 KB tool
// results grow without bound — which is the same defect in the other direction.
//
// The value is a quarter of DefaultContextWindow, stated in tokens rather than
// as a ratio of the window ON PURPOSE: the window is a deploy-time number
// (GOPI_CONTEXT_WINDOW), and a floor that tracks it lets an operator "fix"
// compression by enlarging the window — exactly the "turn the number up"
// non-fix this change forbids. At the deployed 200K window the two readings
// coincide, which is why the number is recognisable; at any other window the
// ceiling stays the one this project measured its tail against.
const compactKeepRecentTokensCap = DefaultContextWindow / compactKeepRecentTokenCapDivisor

// Harness-injection markers. harnessMessagePrefix opens every user message the
// agent framework appends for the model (start/review checkpoints, job and
// truncation notices, carry-forward blocks, task-list nudges) — the marker the
// operator-facing log renders as the `[system]` prefix, and the single source
// of truth referenced by all 18 append sites in this package. It is a
// wire-and-log value, never typed inline (Rule 1b/13): the two messages that
// predate the prefix convention carry their own bracket markers instead, listed
// in harnessMessageMarkers.
//
// THE VALUE IS NOT DEFINED HERE (Rule 14). It is sessionevent.HarnessMessagePrefix,
// in the leaf package the session-log READERS also import — a reader cannot
// import this package (internal/agent imports sessionevent, so the edge would be
// a cycle), and without the shared definition each reader would spell "[system] "
// for itself. This name stays as the alias the append sites and the compaction
// predicate read, so the migration is a rename and not a re-spelling.
const harnessMessagePrefix = sessionevent.HarnessMessagePrefix

// harnessMessageMarkers are the leading markers of the harness-injected user
// messages that do NOT open with harnessMessagePrefix (they predate the
// convention and keep their original wording — the log/first-line contract is
// asserted by tests): the one-shot review-time memory-consult prompt, the
// authoritative committed-conclusions carry-forward block, and the
// self-recording note block re-injected after a compaction.
//
// THE LIST IS NOT DEFINED HERE (Rule 14). It is sessionevent.HarnessMessageMarkers,
// in the leaf package the session-log READERS also import — and that sharing is
// the FIX, not tidying: while this slice lived only here, a reader could consult
// nothing but the `[system] ` prefix, so `chat --replay` typed 12
// `[SELF-RECORD …]` re-injections as if the person had and `session log --turns`
// turned the gaps between them into spurious turns (measured on session
// 80c07a7d: 23 and 17 against 11 operator rows). Now the harness WRITES from the
// same list the readers CLASSIFY by, through sessionevent.IsHarnessInjected.
//
// PARTICIPATE, DO NOT MERGE (Rule 14 / the operator's constraint 4). The three
// markers are three entries in ONE slice, not one new prefix: folding the note
// block into harnessMessagePrefix would spell "this is framework chatter" for a
// message that is the run's OWN distilled record, and it would put the note
// text inside the [system] log contract every harness message keeps. The
// property the READERS need is not the label but the predicate — a
// harness-injected user message is not something the operator typed, so a
// transcript reader must not attribute it to them — and an entry in the shared
// slice buys exactly that, because isHarnessInjectedUserMessage consults it.
var harnessMessageMarkers = sessionevent.HarnessMessageMarkers

// isHarnessInjectedUserMessage reports whether a user-role message was
// appended by the harness (the agent framework itself) rather than typed by
// the operator. Two readers depend on it: a transcript RENDERER (the terminal
// chat surface must not attribute the framework's own follow-ups to the
// operator) and the session-log reader that classifies rows.
//
// The distinction is structural and lives in the message's own first line:
// every framework-injected user message opens with harnessMessagePrefix
// ("[system] ") — the 18 append sites in loop.go, chat_ops.go and reframe.go
// all build their text from that constant, and the two bracket-marked
// messages (the memory-consult checkpoint and the committed-conclusions
// carry-forward block) carry their own markers. Measured against the field
// session (c2eff914): of its 13 user rows, the epic is the ONLY message with
// operator text and ALL the rest carry a harness marker — the session had no
// human follow-up at all, so every "last user" floor in it was a harness
// injection (devdoc/compact-followup-anchor.md).
//
// The content is NOT rewritten and NOT dropped: a harness follow-up still
// reaches the model (job notices, checkpoints and carry-forward blocks are
// load-bearing context); it is only exempt from being a floor.
// IsHarnessInjectedUserMessage is the exported form of the predicate below, for
// a CARRIER that renders the transcript itself: the terminal chat surface must
// not attribute the framework's own user-role follow-ups to the operator, and
// the marker set belongs to this package (one definition — Rule 14). Carriers
// that render nothing (the SSE page) simply do not call it.
func IsHarnessInjectedUserMessage(m deepseek.Message) bool {
	return isHarnessInjectedUserMessage(m)
}

func isHarnessInjectedUserMessage(m deepseek.Message) bool {
	head := strings.TrimLeft(m.Content, " \t\r\n")
	if strings.HasPrefix(head, harnessMessagePrefix) {
		return true
	}
	for _, marker := range harnessMessageMarkers {
		if strings.HasPrefix(head, marker) {
			return true
		}
	}
	// THE COMPOSED FACE THE MARKER LIST CANNOT EXPRESS: a DISPATCHED child's
	// frame with a marked briefing inside it (row 01a188da). It cannot be a list
	// entry — a bare frame is NOT an injection (this profile's tree holds 1118
	// such rows, so declaring it one reclassifies the corpus) — and it is not a
	// simple prefix either, so the loop above cannot see it. Delegated to the
	// leaf's own predicate rather than re-implemented: two spellings of "is this
	// the harness" is exactly the drift this file exists to prevent (Rule 14),
	// and TestBothSidesAgreeOnEveryMarker is the arm that caught the divergence.
	return sessionevent.IsHarnessInjected(head)
}

// Context manages the message history for the agent loop.
type Context struct {
	Messages       []deepseek.Message
	SystemPrompt   string
	ProjectContext string // GOPI.md content injected as 2nd system message (optional)

	// PromptTokens is the authoritative prompt-side token count of the
	// conversation (system prompts + messages) as reported by the API's
	// last response; PromptTokensIndex is the number of Messages covered
	// by that measurement. Together they calibrate EstimatedTokens: covered
	// messages use the measured count, messages added since use the
	// chars/4 heuristic. 0 = never measured (or stale after trimming) —
	// the pure heuristic is used then.
	PromptTokens      int
	PromptTokensIndex int

	// trimmedIDs records tool_use ids already processed by RemoveToolPair
	// (context_trim / memory_feedback useless-slim). A call_id is handled at
	// most once per session (防重复) — the second trim is a silent no-op.
	// Lazily initialized; nil-safe. Single-threaded loop access only.
	trimmedIDs map[string]bool

	// tools is the tool definitions this context's wire carries, with their
	// token cost (context_tools.go owns both). The API bills them as part of
	// the prompt, so an estimate that omits them reports a number the reader
	// cannot compare against prompt_tokens.
	tools toolsEstimate

	// tokenCosts caches the BPE cost of a message or text block, keyed by a
	// fingerprint of its CONTENT (context_tokens.go). The trim loops call the
	// estimate once per removed message, so an uncached full BPE pass would be
	// O(n²) over the history.
	tokenCosts map[uint64]int

	// estimateSystemPrompt is the system prompt the STATUS READING should price
	// when it is not the one Ctx.SystemPrompt holds — the chat wire composes its
	// prompt per request (chatOpsSystemPrompt), so the plain base is not what the
	// server is billed for. It never reaches a request: an estimate-only field is
	// the whole point (context_tokens.go's CalibratedTokens reads it, WireMessages
	// does not).
	estimateSystemPrompt string

	// ToolResultSessionPath is where THIS run's full tool results are recorded —
	// the session JSONL that LogToolResult wrote the uncapped text to. It exists
	// so a capped result's note can name a real recovery route instead of
	// asserting that one exists (Rule 15: a truncation must say where the rest
	// is). Empty means no path was recorded, and the note says THAT.
	ToolResultSessionPath string

	// requestTokens is the LAST reading the send-time token gate took
	// (request_tokens_cap.go), and requestTokensSet says whether one was taken.
	// A status row or a report compares this against what it prints, so the two
	// cannot disagree about the same request without the disagreement being
	// visible. Kept here rather than on the Agent because it describes THIS
	// context's wire.
	requestTokens    RequestTokenCount
	requestTokensSet bool
}

// SetPromptTokens records the API-measured prompt token count for the
// current conversation state. Call after each LLM response — the engine
// then knows the real context size instead of guessing.
func (c *Context) SetPromptTokens(tokens int) {
	c.PromptTokens = tokens
	c.PromptTokensIndex = len(c.Messages)
}

// AddMessage appends a message to the conversation history.
func (c *Context) AddMessage(msg deepseek.Message) {
	c.Messages = append(c.Messages, msg)
}

// AddUserMessage appends a user message to the context.
func (c *Context) AddUserMessage(content string) {
	c.AddMessage(deepseek.Message{Role: "user", Content: content})
}

// AddAssistantMessage appends an assistant message with optional tool calls.
func (c *Context) AddAssistantMessage(content string, toolCalls []deepseek.ToolCall) {
	c.AddAssistantMessageWithThinking(content, toolCalls, "", "")
}

// AddAssistantMessageWithThinking appends an assistant message with optional
// tool calls and the model's thinking block. Thinking MUST be replayed in
// assistant history on the Claude API path — the endpoint rejects history
// that drops thinking blocks (verified live).
func (c *Context) AddAssistantMessageWithThinking(content string, toolCalls []deepseek.ToolCall, thinking string, thinkingSignature string) {
	c.AddMessage(deepseek.Message{
		Role:              "assistant",
		Content:           content,
		ToolCalls:         toolCalls,
		ReasoningContent:  thinking,
		ThinkingSignature: thinkingSignature,
	})
}

// AddToolResult appends a tool result message to the context.
//
// THE ONE INGESTION POINT for a tool result, and therefore THE place the
// per-result token cap is applied (toolresult_cap.go). Every caller — the run
// loop (loop.go:5294), the chat ops loop (chat_ops.go:969) and the prefetch
// seed (chat_prefetch_seed.go:112) — reaches the conversation through this
// function, so the cap cannot be bypassed by a new caller. Two paths build a
// tool message WITHOUT coming through here, and both are deliberately outside
// the cap: resume.go:259 reconstructs history from a checkpoint that was
// already capped when it was first put on the wire, and context.go's orphan
// placeholder is a one-line constant.
func (c *Context) AddToolResult(toolCallID string, result string) {
	c.AddMessage(deepseek.Message{
		Role:       "tool",
		Content:    c.capToolResult(toolCallID, result),
		ToolCallID: toolCallID,
	})
}

// RemoveToolPair removes or replaces an assistant tool_use block (matched by
// its tool_use_id) together with its immediately-following tool result
// message — the generalized 成对 trim behind BOTH the memory_feedback
// useless-vote context slimming (search pairs) and the context_trim tool
// (ANY tool's dead-end result). The tool message is removed together with
// its owning tool_use so no orphaned tool_result can ever survive; when the
// assistant message also carries OTHER tool calls or text it is kept (minus
// the removed call), so sibling tool results in the same batch stay owned and
// the Claude role alternation / tool_use↔tool_result ordering contract is
// preserved.
//
// replaceWith semantics:
//   - replaceWith == "": the pair is DELETED (removed > 0, replaced = false).
//   - replaceWith != "": the pair is KEPT but the tool_result CONTENT is
//     replaced with the one-line conclusion (removed = 0, replaced = true).
//     Message structure and roles are untouched — no alternation risk, and
//     the knowledge survives as a "dead end + why" record (防遗忘).
//
// Dedup (hard): each tool_use_id is processed at most once per session — a
// second trim on the same id is a silent no-op (removed=0, replaced=false).
//
// No-op cases are safe and silent: empty/unknown tool_use_id, an already-
// processed id, or an incomplete pair (tool_use without its result, or vice
// versa) — never partially trim a pair, and never error: the caller's effect
// (feedback vote / context_trim tool result) has already been recorded
// regardless.
func (c *Context) RemoveToolPair(toolUseID, replaceWith string) (removed int, replaced bool) {
	if toolUseID == "" {
		return 0, false
	}
	if c.trimmedIDs == nil {
		c.trimmedIDs = make(map[string]bool)
	}
	if c.trimmedIDs[toolUseID] {
		return 0, false // already processed — 防重复
	}
	// Scan from the most recent message: the call being trimmed is the one
	// the model just saw, but an older identical-ID call would be an equally
	// valid target — any match is a full, correct trim.
	for i := len(c.Messages) - 1; i >= 0; i-- {
		asst := &c.Messages[i]
		if asst.Role != "assistant" {
			continue
		}
		idx := -1
		for k := range asst.ToolCalls {
			if asst.ToolCalls[k].ID == toolUseID {
				idx = k
				break
			}
		}
		if idx == -1 {
			continue
		}
		// Locate the matching tool result immediately after this assistant
		// message (tool results follow their owner in tool-call order and
		// never span an assistant boundary).
		ti := -1
		for k := i + 1; k < len(c.Messages); k++ {
			if c.Messages[k].Role == "tool" && c.Messages[k].ToolCallID == toolUseID {
				ti = k
				break
			}
			if c.Messages[k].Role == "assistant" {
				break
			}
		}
		if ti == -1 {
			return 0, false // incomplete pair — never orphan a tool_use; skip silently
		}
		c.trimmedIDs[toolUseID] = true // processed once — 防重复
		if replaceWith != "" {
			// Replace, don't delete: keep the tool_use + tool_result pair
			// (roles/message structure untouched) but swap the result content
			// for the one-line conclusion — the dead end stays visible as a
			// "don't retry" record, without its bulky output.
			c.Messages[ti].Content = replaceWith
			return 0, true
		}
		// Drop the tool result first (higher index).
		c.Messages = append(c.Messages[:ti], c.Messages[ti+1:]...)
		if len(asst.ToolCalls) == 1 && asst.Content == "" {
			// The assistant message held only this call and no text — remove
			// the whole message.
			c.Messages = append(c.Messages[:i], c.Messages[i+1:]...)
			return 2, false
		}
		// Keep the assistant message; drop just this one tool call.
		asst.ToolCalls = append(asst.ToolCalls[:idx], asst.ToolCalls[idx+1:]...)
		return 1, false
	}
	return 0, false
}

// GetMessages returns messages with system prompt and project context prepended.
// Message order (IMMUTABLE):
//  1. Kernel prompt (SystemPrompt) — always first system message
//  2. GOPI.md content (ProjectContext) — second system message if non-empty
//  3. Conversation messages (user/assistant/tool)
func (c *Context) GetMessages() []deepseek.Message {
	var result []deepseek.Message
	if c.SystemPrompt != "" {
		result = append(result, deepseek.Message{Role: "system", Content: c.SystemPrompt})
	}
	if c.ProjectContext != "" {
		result = append(result, deepseek.Message{Role: "system", Content: c.ProjectContext})
	}
	return append(result, c.Messages...)
}

// WireMessages returns the messages to send to the API. When keepThinking
// is false (the default — P2 thinking-trim), assistant reasoning (thinking
// text) is STRIPPED from the wire: the reasoning was already logged to the
// session JSONL, and LLM reasoning is per-round (the model never sees the
// previous round's thinking unless the harness re-sends it), so replaying it
// costs 1-4K tokens per round for no benefit. Role alternation, text content
// and tool_use blocks are untouched — reasoning is a field, not a message.
// The strip is wire-only: c.Messages keeps ReasoningContent for compaction
// and session logging. When keepThinking is true the messages are returned
// verbatim (A/B control arm, GOPI_KEEP_THINKING=1).
func (c *Context) WireMessages(keepThinking bool) []deepseek.Message {
	msgs := c.GetMessages()
	if keepThinking {
		return msgs
	}
	out := make([]deepseek.Message, len(msgs))
	for i, m := range msgs {
		if m.Role == "assistant" {
			m.ReasoningContent = ""
			m.ThinkingSignature = ""
		}
		out[i] = m
	}
	return out
}

// EstimateTokens estimates the token count of one message on the BPE basis —
// the same basis the endpoint bills and `gopi tool tokens` reports. Tool call
// names, IDs and serialized arguments are counted too, because they ride the
// request.
//
// WHY THIS IS NO LONGER chars/4 (and why the old comment's justification was
// wrong). The retired wording said CJK was "a safe lower bound for triggering
// the trim". It is not: the trigger fires when the estimate EXCEEDS a fraction of
// the window (context_compress.go), so an under-count fires it LATE or never —
// MEASURED 1.78x under on a 35-rune Chinese sentence and 2.24x on a 2 KB Chinese
// aggregate (single Chinese sentences read 2.4-2.6x low), while ENGLISH prose
// reads 1.39x OVER, so the same fallback can also make the trigger fire EARLY. The measured table, the two
// readers this number feeds (the compression's trigger and the compression
// target's fixed floor, context_compress.go) and the send-time cap that then
// refuses the request are on tokenCost in context_tokens.go.
//
// THE OVERHEAD. messageTokenOverhead is added on top of the counted text; it is
// NOT a fudge factor for BPE error (the count is exact) but the template markers
// the server charges around a message that this package does not render. It is
// the SAME value the chars/4 arms used (+1 per message), kept so the estimate is
// continuous with the suite's calibrated arithmetic.
//
// CJK: the previous basis divided by charsPerToken, which is right for English
// prose and wrong for Chinese — MEASURED heur/real 0.45 on a Chinese aggregate
// (2.24x under) while ENGLISH reads 1.389 (1.39x over), so the fallback's error
// changes sign with the content. charsPerToken survives only as
// the tokenizer-unavailable fallback (heuristicTextTokens) — Rule 1/13's single
// named constant is unchanged.
func EstimateTokens(msg deepseek.Message) int {
	return tokenCost(nil, messageWireText(msg), messageTokenOverhead)
}

// EstimatedTokens returns the estimated token count of the full conversation,
// including the system prompt and project context (which GetMessages injects
// on the wire). When SetPromptTokens has recorded an authoritative count for
// the current message range, that count is used for the covered messages and
// only the tail added since is heuristic — so the compaction trigger fires on
// measured size, not guesswork. heuristicTokens() is the pure chars/4
// estimate, the only basis available on BOTH sides of a compaction (which
// resets the measurement).
func (c *Context) EstimatedTokens() int {
	if c.PromptTokens > 0 && c.PromptTokensIndex <= len(c.Messages) {
		total := c.PromptTokens // measured count already includes system prompts
		// c.messageTokens, not EstimateTokens: this loop runs once per round over
		// the post-measurement tail, and EstimateTokens takes no context, so it
		// cannot reach the content-keyed cache (context_tokens.go's tokenCost).
		// Two entry points for one arithmetic is how they drifted before; the
		// cached one is the package's own.
		for _, m := range c.Messages[c.PromptTokensIndex:] {
			total += c.messageTokens(m)
		}
		return total
	}
	total := c.textTokens(c.SystemPrompt) + c.textTokens(c.ProjectContext)
	for _, m := range c.Messages {
		total += c.messageTokens(m)
	}
	// The tool definitions ride the request as a TOP-LEVEL field and the server
	// bills them with the rest of the prompt. They are counted here — and NOT in
	// the measured branch above, where the API's own count already includes
	// them — because context_tools.go measures the array where its size is
	// decided. Leaving them out is the defect this branch existed with: on a
	// resumed session, before the first round, the row read 36k for a prompt the
	// server then charged 131k for.
	return total + c.ToolsTokens()
}

// estimateAfterDroppingHead returns the conversation's estimate once the first
// drop messages are gone, given est — the estimate BEFORE they were dropped.
//
// WHY THIS EXISTS. The trim loops drop one message per iteration and ask
// "are we under the limit yet?". Calling EstimatedTokens() there is O(n) per
// iteration even when every message is cached (n hashes and n map lookups), so the
// loop is O(n²) — and this loop only ever runs on a context that is ALREADY large,
// which is the worst possible place for a quadratic term. With the messages'
// BPE costs cached (messageTokens), the exact new total is one subtraction per
// dropped message.
//
// THE ANCHORED CASE RECOMPUTES, deliberately. When an API measurement covers a
// PREFIX of the array, the total is `measured + tail`, and a message inside that
// prefix does not cost what messageTokens says it costs — the measurement priced
// the whole rendered prefix, markers included. Dropping from the head therefore
// invalidates the subtraction, not just the cache, so that case pays the O(n)
// recompute and stays correct rather than fast.
func (c *Context) estimateAfterDroppingHead(drop, est int) int {
	if c.PredictionAnchored() || drop > len(c.Messages) {
		return c.EstimatedTokens()
	}
	for _, m := range c.Messages[:drop] {
		est -= c.messageTokens(m)
	}
	if est < 0 {
		return 0
	}
	return est
}

// heuristicTokens is the estimate of the conversation (system prompts included)
// with NO dependence on a recorded API measurement — on the BPE basis, not
// chars/4 (that wording described the basis this function no longer uses; its own
// name is the only thing left over from it). It is
// the common basis for comparing context sizes across a mutation: a
// measurement reset (compaction) invalidates the measured base, so a
// before/after verdict must use this function on both sides.
func (c *Context) heuristicTokens() int {
	total := c.textTokens(c.SystemPrompt) + c.textTokens(c.ProjectContext)
	for _, m := range c.Messages {
		total += c.messageTokens(m)
	}
	// The tool definitions ride the request as a TOP-LEVEL field, exactly as in
	// EstimatedTokens above — and they are counted HERE for the same reason,
	// which this function was missing (task 01a1dce2). A savings verdict
	// compares this function on both sides, so leaving ~14K of immovable tool
	// schema out of the denominator made every reported saving larger than the
	// truth, and the same basis is what the compression target's fixed floor is
	// measured on (context_compress.go) — one basis, one arithmetic.
	return total + c.ToolsTokens()
}

// messageUsesToolID reports whether m is an assistant message carrying a
// tool_use with the given id.
func messageUsesToolID(m *deepseek.Message, id string) bool {
	for i := range m.ToolCalls {
		if m.ToolCalls[i].ID == id {
			return true
		}
	}
	return false
}

// pairedToolResultRun counts the consecutive Role=="tool" messages directly
// following index i: the results that belong to the assistant message at i.
func pairedToolResultRun(msgs []deepseek.Message, i int) int {
	n := 0
	for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
		n++
	}
	return n
}

// verifyToolUsePairs is the PROOF of the constraint that
// BuildClaudeRequest's conversion relies on, checked against the source
// message array (before conversion, where the pairing is visible):
//
//	for every assistant message at index i carrying tool_use block id X,
//	the consecutive tool messages directly after it must carry a
//	tool_result for X — i.e. X must be answered inside the run of Role=="tool"
//	messages that immediately follows (the run BuildClaudeRequest coalesces
//	into the single user message that must answer it), and never left
//	unanswered at the END of the array.
//
// It returns the first violation as an error naming the message index and the
// tool_use id. Zero-dep, pure, and cheap enough to run on every outbound
// build (one linear pass, no allocation).
//
// Why this exists at all (the incident it closes, epic 2026-09-22): the
// invariant was previously only ASSERTED in prose — trimCommittedTaskArea's
// comment claimed "the walk is orphan-safe by design", and BuildClaudeRequest
// assumed its input was well formed. Neither held. The trim kept messages[0]
// unconditionally while dropping the window [1, ci) that held messages[0]'s
// tool_result, the wire carried a head assistant tool_use with no result
// after it, and the API answered HTTP 400 "messages.1: `tool_use` ids were
// found without `tool_result` blocks immediately after:
// call_00_ET_TBCTnLz8k12TGe2ZEVxl2105" — killing the live run and, because
// the damaged array was what the raw log recorded, killing every resume too.
// An assertion is not a proof; this is the proof, and it is wired into the
// loop so a violation FAILS CLOSED (the round errors out loudly) instead of
// being sent.
func verifyToolUsePairs(msgs []deepseek.Message) error {
	for i := range msgs {
		m := &msgs[i]
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			answered := false
			for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
				if msgs[j].ToolCallID == tc.ID {
					answered = true
					break
				}
			}
			if !answered {
				return fmt.Errorf(
					"tool_use %q at messages[%d] has no tool_result in the immediately following tool message (Claude Messages pairing invariant)",
					tc.ID, i)
			}
		}
	}
	return nil
}

// orphanToolResultPlaceholder is the tool_result text the pairing repair
// writes for a tool_use whose result is missing from the context. It is
// deliberately not error-shaped: the tool did not necessarily fail — the
// round's result is gone (dropped by a context compaction or a trim
// boundary), and a model told "error" re-runs the tool or apologises for a
// failure that never happened.
const orphanToolResultPlaceholder = "[gopi] tool result unavailable — this call's output is not in the context (the round was interrupted before it returned, or its result was dropped by a context compaction); the call did not necessarily fail. Re-run the tool if you still need the output."

// repairToolUsePairs is the fail-closed REPAIR for the Claude Messages
// pairing invariant on the SOURCE message array (before conversion): every
// assistant message carrying a tool_use whose id is answered by no tool
// message directly after it gets the missing half synthesized as a tool
// message, inserted immediately after the owning assistant message.
//
// It never removes anything: the conversation the user and the model already
// saw stays intact, only the absent result is supplied. It returns the
// possibly-REGrown array (the caller must use the return value: inserting a
// message changes the slice length, and a Go slice header is passed by value)
// together with the repaired tool_use ids — empty when the array was already
// sound, in which case the returned slice is the caller's, untouched.
//
// The repair exists because the invariant cannot be asserted: it is broken by
// the harness's OWN context management, not by a malformed conversation
// (measured 2026-09-22 — the committed-region trim kept an unanswered head
// tool_use, the API answered HTTP 400, and the damaged array had already been
// recorded as the resume checkpoint). See verifyToolUsePairs for the proof
// half and internal/deepseek/toolpair.go for the wire-side net.
func repairToolUsePairs(msgs []deepseek.Message) ([]deepseek.Message, []string) {
	var repaired []string
	for i := 0; i < len(msgs); i++ {
		m := &msgs[i]
		if m.Role != "assistant" || len(m.ToolCalls) == 0 {
			continue
		}
		answered := map[string]bool{}
		run := 0
		for j := i + 1; j < len(msgs) && msgs[j].Role == "tool"; j++ {
			run++
			if msgs[j].ToolCallID != "" {
				answered[msgs[j].ToolCallID] = true
			}
		}
		missing := 0
		for k := range m.ToolCalls {
			if !answered[m.ToolCalls[k].ID] {
				missing++
				repaired = append(repaired, m.ToolCalls[k].ID)
			}
		}
		if missing == 0 {
			continue
		}
		// Build the inserts first, then splice — so the walk's indices stay
		// valid for the nested repairs of later messages.
		inserts := make([]deepseek.Message, 0, missing)
		for k := range m.ToolCalls {
			if !answered[m.ToolCalls[k].ID] {
				inserts = append(inserts, deepseek.Message{
					Role:       "tool",
					Content:    orphanToolResultPlaceholder,
					ToolCallID: m.ToolCalls[k].ID,
				})
			}
		}
		at := i + 1 + run
		grown := make([]deepseek.Message, 0, len(msgs)+len(inserts))
		grown = append(grown, msgs[:at]...)
		grown = append(grown, inserts...)
		grown = append(grown, msgs[at:]...)
		msgs = grown
		// The loop's next index is i+1 (the first message of the run, or the
		// first insert when the run was empty) — none of which is an
		// assistant tool_use, so no index fixup is needed beyond skipping the
		// messages we just consumed.
		i = at - 1
	}
	return msgs, repaired
}
