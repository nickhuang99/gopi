package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

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

// DefaultCompactTriggerRatio is the fraction of the context window at which
// trimming kicks in. When the estimated token count exceeds this fraction of
// the window before an API call, the oldest messages are trimmed automatically
// (fold → compact → TrimToFit, all on this single limit).
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
// removed by compaction (GOPI_COMPACT_KEEP_RECENT). Aligned with
// maxVerbatimToolResults: the model is still working from the recent tail.
const DefaultCompactKeepRecent = 8

// Tool-result folding policy (R1): tool results are the context-growth driver
// (a 16KB bash output is ~4K tokens), but the model rarely re-reads OLD ones
// — when it needs the data again it re-runs the command. When the context is
// over budget, tool results older than maxVerbatimToolResults with more than
// maxToolResultLines are collapsed into one-line stubs
// (Context.summarizeOldToolResults). The most recent results stay verbatim:
// the model is still working from them.
const (
	// maxVerbatimToolResults is how many of the most recent tool results
	// stay verbatim during folding.
	maxVerbatimToolResults = 8
	// maxToolResultLines is the line count above which an OLD tool result
	// is collapsed into a one-line stub.
	maxToolResultLines = 20
	// maxToolResultSummaryChars caps the first line kept in a stub, so a
	// single pathological long line cannot defeat the collapse.
	maxToolResultSummaryChars = 120
)

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
// property the ANCHOR POLICY needs is not the label but the predicate — a
// harness-injected user message must never become TakeOldest's "last user"
// floor — and an entry in the shared slice buys exactly that, because
// isHarnessInjectedUserMessage consults it.
var harnessMessageMarkers = sessionevent.HarnessMessageMarkers

// AnchorStop reasons — WHY TakeOldest's deletion pass stopped before the
// estimate reached maxTokens. Each value is a named constant because the same
// string is written into the anchor log line (compact.go) and asserted on by
// the compaction tests: a bare literal at either site is one rename away from
// a silent mismatch (Rule 1b/13).
const (
	// anchorStopLastUser is the instruction floor: a REAL instruction the
	// operator issued (the epic is index 0 and never anchors).
	anchorStopLastUser = "last user"
	// anchorStopLastVisibleAssistant is the reply floor: the last assistant
	// message with visible content (the model's most recent answer).
	anchorStopLastVisibleAssistant = "last visible assistant"
	// anchorStopMemoryBlock is set when TakeOldest refused to compress a
	// region that holds nothing but compaction memory blocks (livelock guard).
	anchorStopMemoryBlock = "memory block floor"
)

// isNonCompressibleMessage reports whether asking an LLM to distill this
// message would be a wasted call: a compaction memory block (distilling the
// compaction's own output converges on nothing — the reported livelock), or a
// self-recording note block (an LLM distillation of the model's OWN one-line
// distillation, whose verbatim copy is re-injected anyway — see selfrecord.go).
//
// IT IS NOT A STOP FLOOR AND NOT AN EXEMPTION FROM DELETION (the epic's hard
// constraint: TakeOldest's stop conditions — lastUser / lastVisibleAssistant /
// keepRecent / protected — gain nothing, and the re-injected note block is not a
// new anchor target). The deletion pass takes such a message like any other
// message when there is real material around it; what this predicate decides is
// the SINGLE question windowIsMemoryBlocks has always asked — "is there anything
// in this window worth a summarizer call?" — so a window holding only these
// stops by the EXISTING anchorStopMemoryBlock path (structural no-op, zero LLM
// calls).
//
// The record itself does not depend on that: a note whose message is deleted is
// re-injected from this run's note list (Agent.ensureSelfRecordPresent), which
// is why discarding the message is safe and why no exemption is needed.
func isNonCompressibleMessage(m deepseek.Message) bool {
	return isMemorySummaryMessage(m) || isSelfRecordMessage(m)
}

// isHarnessInjectedUserMessage reports whether a user-role message was
// appended by the harness (the agent framework itself) rather than typed by
// the operator. It is the predicate behind the anchor policy in TakeOldest:
// a harness follow-up is NOT an instruction, so it must not become the
// "last user" stop floor (see the anchor discussion there).
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

	// AnchorStop records why TakeOldest stopped early on an anchor during
	// the most recent call: "last user" / "last visible assistant". Empty
	// when no anchor stopped it. Set/read by the single-threaded agent loop
	// (or tests) only.
	AnchorStop string

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

// EstimateTokens estimates the token count of a message using a chars/4
// heuristic (roughly 4 characters per token for mixed text). Tool call names,
// IDs and serialized arguments are counted too. CJK text is under-estimated
// by this rule of thumb, but it is a safe lower bound for triggering the
// trim; the API-reported usage remains authoritative.
func EstimateTokens(msg deepseek.Message) int {
	n := len(msg.Role) + 2 + utf8.RuneCountInString(msg.Content)
	for _, tc := range msg.ToolCalls {
		n += utf8.RuneCountInString(tc.Name) + utf8.RuneCountInString(tc.ID)
		if args, err := json.Marshal(tc.Arguments); err == nil {
			n += utf8.RuneCountInString(string(args))
		} else {
			n += utf8.RuneCountInString(tc.RawArgs)
		}
	}
	return n/charsPerToken + 1
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
		for _, m := range c.Messages[c.PromptTokensIndex:] {
			total += EstimateTokens(m)
		}
		return total
	}
	total := 0
	if c.SystemPrompt != "" {
		total += utf8.RuneCountInString(c.SystemPrompt)/charsPerToken + 1
	}
	if c.ProjectContext != "" {
		total += utf8.RuneCountInString(c.ProjectContext)/charsPerToken + 1
	}
	for _, m := range c.Messages {
		total += EstimateTokens(m)
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

// heuristicTokens is the pure chars/4 estimate of the conversation (system
// prompts included) with NO dependence on a recorded API measurement. It is
// the common basis for comparing context sizes across a mutation: a
// measurement reset (compaction) invalidates the measured base, so a
// before/after verdict must use this function on both sides.
func (c *Context) heuristicTokens() int {
	total := 0
	if c.SystemPrompt != "" {
		total += utf8.RuneCountInString(c.SystemPrompt)/charsPerToken + 1
	}
	if c.ProjectContext != "" {
		total += utf8.RuneCountInString(c.ProjectContext)/charsPerToken + 1
	}
	for _, m := range c.Messages {
		total += EstimateTokens(m)
	}
	return total
}

// TakeOldest removes the oldest messages until the estimated token count fits
// within maxTokens and RETURNS them, so the caller can summarize them (memory
// compaction) instead of discarding. Drop semantics are identical to
// TrimToFit: never leaves an orphaned "tool" message at the head, and the
// system prompt is never trimmed (it is injected by GetMessages). The last
// keepRecent messages are never removed — the deletion loop stops at
// len(Messages) <= keepRecent+1, so a single giant recent message cannot
// blow the tail away.
//
// Two further floors stop the deletion early (the estimate may stay above
// maxTokens — deliberately): messages in protected (memory_store rounds)
// and the two anchors — the last user message and the last visible assistant
// message (Content non-empty; thinking/tool_calls-only do not count) are
// never deleted (Hermes #10896/#29824: compaction must not swallow the
// current instruction or the last visible reply). Exception: the first
// message of the history (the epic task input at index 0) is the compaction
// boundary by design — the summary replaces it — so index 0 is never an
// anchor. Later user messages (job notices, continuations, follow-up
// instructions) anchor normally.
//
// A LIVE memory-summary block is not an anchor: a compaction of its own
// output is a no-op that would strand the loop (see the livelock note below).
//
// LIVELOCK GUARD (2026-09-17): the anchor floors can pin the compressible
// window to a single message while the estimate stays far above the target
// (field report, session c2eff914: the previous compaction's memory block sat
// at index 1, the anchors sat right behind it, and every round summarized
// exactly ONE message — the cycle
// "anchored at last user" / "summarized 1 oldest message(s)" /
// "assertion: after ≤ target+summary = false" repeated forever, each round
// paying a summarizer call and prepending another block). The deletion loop
// therefore refuses to keep eating the history once ONLY the memory blocks
// remain between the head and the floors: summarizing them converges on
// nothing (the summary is itself a block), so the loop stops, AnchorStop
// reports "memory block floor", and the caller treats the window as
// structurally unusable (structural no-op backoff + bounded trim) instead of
// paying an LLM call per round.
//
// The returned slice is in original order; the caller must not use the
// returned messages after mutating the context further.
func (c *Context) TakeOldest(maxTokens int, keepRecent int, protected map[int]bool) []deepseek.Message {
	if keepRecent < 0 {
		keepRecent = 0
	}
	c.AnchorStop = ""
	lastUser := -1
	lastVisibleAssistant := -1
	for i := len(c.Messages) - 1; i >= 1; i-- { // index 0 = epic, never anchors
		m := c.Messages[i]
		// A harness-injected follow-up is not an instruction: it must not
		// become the instruction floor. Injected messages are appended
		// whenever the framework has something to say (start checkpoint, job
		// notice, truncation notice, carry-forward block, end-turn nudge), so
		// treating the newest one as "the current instruction" pins the
		// compressible window to the sliver directly behind the head — the
		// field defect this policy fixes (session c2eff914: the checkpoint
		// injections the run received early held the floor for the whole run
		// while the body of work sat behind them, and every round summarized
		// exactly ONE message while the estimate stayed above the limit). The
		// operator's own instruction — the epic at index 0, or a real
		// follow-up typed later — still anchors normally.
		if m.Role == "user" && lastUser == -1 && !isHarnessInjectedUserMessage(m) {
			lastUser = i
		}
		if m.Role == "assistant" && m.Content != "" && lastVisibleAssistant == -1 {
			lastVisibleAssistant = i
		}
	}
	var taken []deepseek.Message
	offset := 0 // original index of the current head message
	for len(c.Messages) > keepRecent+1 && c.EstimatedTokens() > maxTokens {
		if protected != nil && protected[offset] {
			break // protected round (memory_store distillation)
		}
		if offset == lastUser || offset == lastVisibleAssistant {
			if offset == lastUser {
				c.AnchorStop = anchorStopLastUser
			} else {
				c.AnchorStop = anchorStopLastVisibleAssistant
			}
			break
		}
		// Livelock guard: when EVERY message the deletion can reach before
		// its stop floors is a compaction memory block, there is no
		// compressible material left — summarizing those blocks feeds the
		// compaction its own output (one LLM call to produce one more block,
		// the observed cycle). See windowIsMemoryBlocks.
		if c.windowIsMemoryBlocks(offset, lastUser, lastVisibleAssistant, keepRecent) {
			c.AnchorStop = anchorStopMemoryBlock
			break
		}
		// Drop the assistant message together with any tool results that
		// immediately follow it, so the remaining history never starts
		// with an orphaned tool message.
		for len(c.Messages) > keepRecent+1 && c.Messages[1].Role == "tool" {
			taken = append(taken, c.Messages[0])
			c.Messages = c.Messages[1:]
			offset++
		}
		if len(c.Messages) <= keepRecent+1 {
			break
		}
		taken = append(taken, c.Messages[0])
		c.Messages = c.Messages[1:]
		offset++
	}
	// Safety net: never leave a lone orphaned tool result as the head.
	for len(c.Messages) > 0 && c.Messages[0].Role == "tool" {
		taken = append(taken, c.Messages[0])
		c.Messages = c.Messages[1:]
	}
	return taken
}

// windowIsMemoryBlocks reports whether every message this deletion pass could
// still remove is one the summarizer may never be asked to distill — a
// compaction memory block, or a self-recording note block. The whole region
// from the current head up to the FIRST stop floor (an anchor floor or the
// keep-recent count floor) then holds no compressible material at all.
//
// Such a window is not worth a summarizer call: the only thing left to distill
// is the compaction's own earlier output (or a copy of the model's own note), so
// the pass would spend an LLM round-trip to prepend one more memory block and
// re-create the identical window on the next round — the reported cycle (field
// log: "anchored at last user" / "summarized 1 oldest message(s)" on 305 rounds
// of one session, the two counts equal). The deletion loop stops instead and
// records anchorStopMemoryBlock, so the caller treats the window as a structural
// no-op (cheap bounded trim) rather than one LLM call per round.
//
// This is NOT a stop floor and NOT a new exemption: the predicate decides
// nothing about WHEN the pass stops (that is still the anchors, the keep-recent
// count and the protected store rounds, unchanged), only WHICH material is
// worth an LLM call. A note block that falls inside a window with real
// compressible material is simply taken along with it — and is excluded from
// the summarizer's input, so the pass discards it silently; the record itself is
// re-injected by selfrecord.go's carrier, which is what makes discarding it safe
// (see ensureSelfRecordPresent).
//
// INDEX SPACE: the loop deletes from the FRONT, so c.Messages[0] is original
// index `offset` — every position below is expressed as a CURRENT index
// (anchors arrive as original indices and are rebased by `offset`), because the
// current length shrinks with each deletion while the anchor floors do not.
func (c *Context) windowIsMemoryBlocks(offset, lastUser, lastVisibleAssistant, keepRecent int) bool {
	if keepRecent < 0 {
		keepRecent = 0
	}
	// First current index the loop cannot cross, from each floor.
	stop := len(c.Messages) - keepRecent - 1
	for _, anchor := range []int{lastUser, lastVisibleAssistant} {
		if anchor < 0 {
			continue
		}
		if cur := anchor - offset; cur < stop {
			stop = cur
		}
	}
	if stop > len(c.Messages) {
		stop = len(c.Messages)
	}
	if stop <= 0 {
		// The floor stops the loop at or before the current head: there is no
		// window to judge, and claiming the blocks-only shape here would abort
		// a deletion the anchor/keep-recent checks already bound.
		return false
	}
	for i := 0; i < stop; i++ {
		if !isNonCompressibleMessage(c.Messages[i]) {
			return false
		}
	}
	return true
}

// summarizeOldToolResults collapses old tool results into one-line stubs
// (R1): tool messages older than the most recent maxVerbatimToolResults whose
// content exceeds maxToolResultLines are replaced with a stub of their first
// line. Recent results stay verbatim. Returns the number of collapsed
// messages. Cheap O(n) pass, no LLM involved, never removes messages — so no
// orphaned-tool-result risk. The stub keeps the command's exit line (bash
// results start with "exit=N") so the model still sees the outcome.
func (c *Context) summarizeOldToolResults() int {
	seen := 0
	folded := 0
	for i := len(c.Messages) - 1; i >= 0; i-- {
		m := &c.Messages[i]
		if m.Role != "tool" {
			continue
		}
		seen++
		if seen <= maxVerbatimToolResults {
			continue
		}
		lines := strings.Count(m.Content, "\n") + 1
		if lines <= maxToolResultLines {
			continue
		}
		first := strings.TrimSpace(m.Content)
		if j := strings.IndexByte(first, '\n'); j >= 0 {
			first = first[:j]
		}
		if len(first) > maxToolResultSummaryChars {
			first = first[:maxToolResultSummaryChars] + "…"
		}
		m.Content = fmt.Sprintf("[tool result summarized: was %d lines — first line: %q]", lines, first)
		folded++
	}
	return folded
}

// trimToFitStickyHead brings the conversation back under maxTokens with a
// CACHE-AWARE eviction, replacing the plain sliding-window trim at the chat/
// task call site (TrimToFit itself is kept for callers that need it verbatim).
// Measured reason (session a09b91ae, 2026-09-30; numbers in
// devdoc/chat-cache-hit-attribution.md). That chat's context sat just above the
// trim limit because of a fixed per-turn overhead (system prompt + tool schema
// ≈ 31K tokens re-sent on every request), so trimContext fired on EVERY
// request. `TrimToFit(limit)` removed only the overshoot's worth, the round's
// own append pushed the context straight back over the limit, and the next trim
// deleted from the NEW head — the head crept forward a few messages per round
// like a sliding window. Every one of those requests therefore re-sent the
// whole conversation past the (frozen) system prompt as a MISS. Wire truth,
// joined 684/685 requests to their actual payload: the 41 requests whose
// message list SHRANK at the head versus the previous request carried 60.9% of
// the session's 7.9M miss tokens, and 49 of the 65 big-miss requests followed a
// shrinking list. Each such request paid a multi-second prefill, which is what
// turned ordinary rounds into the 473s turn in the console log.
//
// THE DEFECT WAS THE NIBBLING, NOT THE EVICTION. Deleting a few messages per
// round pays a cold prefix on EVERY round and can never amortize the loss. The
// fix removes head messages only down to targetTokens — one amortized eviction
// that costs ONE round's prefill, after which the following rounds are served
// from cache again until the context grows back to the limit.
//
// Recency is preserved deliberately: the victim is the oldest message, never
// the newest. Dropping the model's own most recent turn would break the round
// it is in the middle of, which no cache saving buys back.
//
// THE HISTORY IS NEVER EMPTIED — the same floor TrimToFit carries, for the same
// measured reason (9b9264b9: an empty slice reaches the wire as
// `"messages":null` and the endpoint answers HTTP 422). Two guards enforce it,
// both copied from TrimToFit, and both load-bearing:
//
//   - a whole head RUN (the head plus every tool result immediately following
//     it) is dropped in ONE step, so no iteration can end between a tool_use and
//     its tool_result — which is also what guarantees the surviving head is
//     never an orphaned tool result;
//   - the run that would consume the ENTIRE history is refused, so the estimate
//     may stay above maxTokens: a valid over-budget request (the newest message
//     IS the round being served) is strictly better than an emptied, i.e.
//     malformed, one.
func (c *Context) trimToFitStickyHead(maxTokens, targetTokens int) int {
	if maxTokens <= 0 {
		return 0
	}
	if targetTokens <= 0 || targetTokens > maxTokens {
		targetTokens = maxTokens
	}
	removed := 0
	for len(c.Messages) > 1 && c.EstimatedTokens() > targetTokens {
		drop := 1 + pairedToolResultRun(c.Messages, 0)
		if drop >= len(c.Messages) {
			break // never empty the history — see the doc comment
		}
		c.Messages = c.Messages[drop:]
		removed += drop
	}
	return removed
}

// TrimToFit removes the oldest messages until the estimated token count fits
// within maxTokens. The system prompt is never trimmed (it is injected by
// GetMessages, not stored in Messages). Trimming never leaves an orphaned
// "tool" message at the head of the history: if the new head would be a tool
// result whose owning assistant tool-call message was removed, that result is
// removed along with it (the API rejects tool messages without a preceding
// tool_calls message). Returns the number of messages removed.
//
// THE HISTORY IS NEVER EMPTIED. A trim trades old context for budget; it must
// never trade away the request itself. Two guards enforce that, and both are
// load-bearing:
//
//   - a whole head RUN (the head plus every tool result immediately following
//     it) is dropped in ONE step, so no iteration can end between a tool_use
//     and its tool_result;
//   - the run that would consume the ENTIRE history is refused — the estimate
//     may stay above maxTokens, deliberately, exactly like TakeOldest's anchor
//     floors.
//
// Measured defect (2026-09-30, dumps/20260930-194502-1.dump.json): the old
// two-step loop deleted the head and relied on the NEXT iteration to delete
// the tool results it had orphaned. On a short multi-round chat — history is
// exactly one tool round (user, assistant tool_use, tool result) while the
// bulk of the estimate is the system prompt + project context — the floor
// stopped the loop between those two steps, the safety net then removed the
// stranded tool result, and c.Messages became EMPTY. ClaudeRequest.Messages
// carries no `omitempty`, so the empty slice serialized as
//
//	{"model":"deepseek-flash","max_tokens":16384,"messages":null}
//
// and the endpoint answered HTTP 422 "Failed to deserialize ... messages:
// invalid type: null, expected a sequence". Every chat turn that needed a
// SECOND round died there, deterministically (the first round has one message
// and was saved by the len>1 guard), which is why only round 2+ ever hit it.
//
// The API measurement is invalidated when anything is removed: it was taken
// over the OLD, larger range, so its index no longer bounds this array and
// EstimatedTokens would score the survivors against a count that included the
// removed messages. Same semantics as compaction (compact.go: "the API
// measurement ... must be invalidated or the next round re-triggers on the
// stale count").
func (c *Context) TrimToFit(maxTokens int) int {
	removed := 0
	for len(c.Messages) > 1 && c.EstimatedTokens() > maxTokens {
		drop := 1 + pairedToolResultRun(c.Messages, 0)
		if drop >= len(c.Messages) {
			break // never empty the history — see the doc comment
		}
		c.Messages = c.Messages[drop:]
		removed += drop
	}
	// Safety net: never leave a lone orphaned tool result as the head. Bounded
	// by the same floor as the loop above, so it cannot empty the history
	// either.
	for len(c.Messages) > 1 && c.Messages[0].Role == "tool" {
		c.Messages = c.Messages[1:]
		removed++
	}
	if removed > 0 {
		c.PromptTokens = 0
		c.PromptTokensIndex = 0
	}
	return removed
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
