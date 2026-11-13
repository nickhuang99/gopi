package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/thinkmode"
	"github.com/nick/gopi/internal/tools"
)

// chatOpsSystemPrompt composes the tool-enabled chat path's system prompt: the
// FULL tool-routing prompt (the model must know the tools exist) with the
// CHAT-SURFACE block appended (chat_context.go) — where this process is, what
// this surface is, and how to read its own screen with the tools it has.
//
// It is appended rather than registered in promptSections because the registry
// is the TASK prompt: adding a chat block there would tell a task run it is a
// chat console. The two paths share the BLOCK (one Render function for the
// static half, ONE chatSessionBlock for the per-run half, one set of runtime
// values), not the assembly.
//
// The parameter is the AGENT rather than the pre-rendered base string, for the
// same reason assembleChatSystemPrompt now takes it: the block's per-run half
// names THIS process's own transcript path, which lives on the Agent — a
// string-only signature could only ever carry the static half, and the two chat
// paths would then disagree about whether the run was told where its own log
// is. The base is read from a.SystemPrompt here, so the composition stays in one
// function.
//
// The empty-prompt case returns the chat block alone, so a caller that built an
// Agent without a SystemPrompt (or passed a nil Agent) still gets a chat that
// knows where it is; and so the block can never be lost to a join that prepended
// a separator to nothing.
func chatOpsSystemPrompt(a *Agent) string {
	block := chatContextRulesFor(true) + chatSessionBlock(a) + chatSurfaceFactRules(a)
	// The unattended sentence rides AFTER the chat block on this path too, so
	// the tool-enabled and the plain carrier describe one run the same way: a
	// run that declared itself unattended says so whichever toolset it carries
	// (chat_unattended.go owns the condition — one gate, two callers).
	if extra := chatUnattendedBlockOpen(a); extra != "" {
		block += promptSectionSeparator + extra
	}
	var base string
	if a != nil {
		base = a.SystemPrompt
	}
	if strings.TrimSpace(base) == "" {
		return maybeAppendChatSelfRecordRule(block)
	}
	return maybeAppendChatSelfRecordRule(base + promptSectionSeparator + block)
}

// Tool-enabled chat mode ("chat + operations", claude-code-style): the chat
// REPL answers free-form user requests through a bounded ReAct loop with the
// FULL GoPi toolset, so real work (read/fix files, run bash, search the web)
// happens inside a conversation instead of requiring a separate one-shot
// `gopi <prompt>` task dispatch. Lives here rather than loop.go to keep the
// task-loop file from growing further; it reuses the loop's private
// machinery (chatOnce, executeToolBatch, trimContext, withTick) as the
// simplified Agent.Chat already does.

// chatOpsEndTurnContinuation is the one-shot message injected when a
// tool-enabled chat turn ends WITHOUT tool calls but with no substantive
// answer — the model may have concluded "I'm done" on reflex. Gives it one
// continuation shot; a second degenerate round is accepted as-is.
const chatOpsEndTurnContinuation = harnessMessagePrefix + "Continue: you have tools available (read/bash/edit/code_search/…). " +
	"Use them if the request needs real work — only reply without tool calls when the answer is complete."

// chatOpsTurnStoreMarker opens the per-turn memory checkpoint message. It is
// named (Rule 13) so the message and any assertion about it share ONE
// definition instead of two copies of the same prose drifting apart — the
// same discipline chatOpsRoundsWarnMarker applies to the wrap-up warning.
const chatOpsTurnStoreMarker = "CHAT TURN CHECKPOINT"

// chatOpsTurnStoreCheckpoint is the ONE-SHOT per-TURN memory(action=store)
// checkpoint of the tool-enabled chat path — the chat counterpart of the task
// loop's completionCheckpointMessage (loop.go). It rides the EXISTING
// message-injection channel (a harness user message in Ctx.Messages, logged
// 1:1 to the session JSONL like roundBoundaryInjections) — never a new
// promptSection, never a new tool (Rule 3).
//
// It is a HARNESS message (the chatOpsEndTurnContinuation family), so the
// budget rule that governs a harness line holds here too: it is bounded by
// CONSTRUCTION, not by the model's good behaviour — maybeChatOpsStoreCheckpoint
// fires it at most once per turn, so a turn adds at most ONE extra line.
var chatOpsTurnStoreCheckpoint = fmt.Sprintf(harnessMessagePrefix+
	"%s: this turn has done real work with tools and has not stored anything yet. "+
	"Before your final answer, call memory(action=store) exactly once to persist what this turn "+
	"actually learned or produced — a distilled lesson, a reusable recipe, a pitfall avoided "+
	"(never a raw log of the steps). Skip it only when the turn produced nothing worth keeping; "+
	"then answer the user.",
	chatOpsTurnStoreMarker)

// maybeChatOpsStoreCheckpoint injects the per-turn store checkpoint and
// returns whether it did. It is the gate-aware chat half of the task-end
// memory_store enforcement (对称 with maybeEnforceCompletionStore), with FOUR
// conditions, all of them required:
//
//	gate    — a.Hippocampus == nil ⇒ no-op. The nil host IS the
//	          GOPI_HIPPOCAMPUS-off guarantee (devdoc/hippocampus.md §4.2 口径 1):
//	          with the gate off this function cannot inject anything, so chat
//	          behaviour is byte-identical to today's.
//	work    — turnToolRounded: the turn actually ran a tool round. A turn
//	          that only talked has nothing to store, and prompting it would be
//	          a fabricated write (the checkpoint's entire value is that it
//	          names a real knowledge gain, so it must not fire without one).
//	latches — turnStored (already fired this turn: ONE shot per TURN,
//	          never per round) and memoryStored (a store already succeeded —
//	          in this turn or an earlier turn of the same persistent session —
//	          so a second one is the spam the latch exists to stop).
//	          turnStored is set BEFORE the injection so a turn whose
//	          store call is later rejected does NOT get a second prompt.
func (a *Agent) maybeChatOpsStoreCheckpoint() bool {
	// A nil host IS the gate-off case (devdoc/hippocampus.md §4.2 口径 1): with
	// GOPI_HIPPOCAMPUS unset no host is built, so this returns before touching
	// anything and chat behaviour is byte-identical to today's.
	if a.Hippocampus == nil {
		return false
	}
	if a.Hippocampus.turnStored || a.memoryStored || !a.Hippocampus.turnToolRounded {
		return false
	}
	a.Hippocampus.turnStored = true
	a.Ctx.AddUserMessage(chatOpsTurnStoreCheckpoint)
	// Auditability, identical to roundBoundaryInjections /
	// maybeInjectTaskStartSearch: an injected wire message that is not in the
	// session JSONL is indistinguishable from a missing one, so a Rule 11
	// check reads the checkpoint as a logged `user` event.
	if a.SessionLog != nil {
		a.SessionLog.LogUser(newUUIDv4(), chatOpsTurnStoreCheckpoint)
	}
	if a.Logger != nil {
		a.Logger.Printf("🧠 chat turn checkpoint: asking for one memory_store before the final answer")
	}
	return true
}

// stampHippocampusRound labels this round with its round number. EVERY host
// decision of the round (the retrieval launched at the boundary, the level, the
// Lv-0 audit record) carries the value, so a chat session's archive trail can be
// ordered; without it the chat path archived every brief under round 0 — a round
// number no round ever has. It MUST be called from the round loop rather than
// from the shared boundary helper, because the helper READS the stamp and only
// the loop knows which round it is (the task loop's runEngine stamps its own).
//
// The counter is SESSION-cumulative, not per-turn: a persistent chat shares ONE
// session log across turns (PersistChatSession), so a per-turn counter restarts
// at 1 and repeats round numbers inside the same log — measured on the real
// binary as the archive trail [1,2,3,1,1,2] for a three-turn session, which is
// as unordered as the all-zero trail it replaced.
//
// A nil host is the gate-off case and the whole method is a no-op there, which is
// what keeps GateOff byte-identical rather than merely quiet.
func (a *Agent) stampHippocampusRound() {
	if a.Hippocampus == nil {
		return
	}
	a.Hippocampus.sessionRound++
	a.hippocampusRound = a.Hippocampus.sessionRound
}

// markChatOpsToolRound records that the turn at hand ran a real tool round —
// the "did work" half of the per-turn checkpoint's predicate. No host (the gate
// off) ⇒ nothing to record and nothing that could ever read it.
func (a *Agent) markChatOpsToolRound() {
	if a.Hippocampus == nil {
		return
	}
	a.Hippocampus.turnToolRounded = true
}

// resetChatOpsTurnCheckpoint clears the per-turn checkpoint latches at the top
// of a ChatOps turn. Without the per-turn reset a persistent session
// (PersistChatSession shares ONE agent and ONE session log across turns) would
// inherit the previous turn's "already stored", and turn 2 would silently lose
// its checkpoint. No host (the gate off) ⇒ no state ⇒ no-op.
func (a *Agent) resetChatOpsTurnCheckpoint() {
	if a.Hippocampus == nil {
		return
	}
	a.Hippocampus.turnToolRounded = false
	a.Hippocampus.turnStored = false
}

// maxChatOpsToolRounds caps a single tool-enabled chat turn's ReAct rounds —
// a safety net against a model that keeps issuing tool calls forever.
// Generous (a turn of real work: search → read → edit → verify fits easily);
// the task loop by contrast has no cap at all (model decides). On hitting
// the cap the turn fails with an error — the REPL surfaces it and the user
// can /clear or reseed.
const maxChatOpsToolRounds = 40

// chatOpsRoundsWarnLead is how many rounds BEFORE the cap the turn warns. The
// cap itself is invisible from the outside until the moment it fires, and by
// then the turn has spent real money (measured: session bbc4efd8 ran 39 tool
// rounds, ~1.5M prompt tokens, and the FIRST thing the operator ever learned
// about the cap was the process ending). A lead of a few rounds is enough for
// the model to wrap up with what it has and for the operator to see it coming.
const chatOpsRoundsWarnLead = 5

// chatOpsRoundsWarnAt is the 1-based round index at which the warning fires:
// the last chatOpsRoundsWarnLead rounds of the budget.
const chatOpsRoundsWarnAt = maxChatOpsToolRounds - chatOpsRoundsWarnLead

// chatOpsRoundsWarnMarker opens the wrap-up warning. It is named (Rule 13) so
// the message and any assertion about it share ONE definition instead of two
// copies of the same prose drifting apart.
const chatOpsRoundsWarnMarker = "Round budget almost spent"

// chatOpsRoundsWarnMessage is the round-boundary inbox message injected at
// chatOpsRoundsWarnAt. It is a HARNESS message (the chatOpsEndTurnContinuation
// family), not user input: it tells the model the budget is nearly spent so it
// can answer with what it already has, which converts the cap from "the turn
// dies" into "the turn lands a partial answer".
var chatOpsRoundsWarnMessage = fmt.Sprintf(harnessMessagePrefix+
	"%s: this turn stops after %d tool rounds. Stop calling tools and answer now "+
	"with what you have — say plainly what is verified and what is not.",
	chatOpsRoundsWarnMarker, maxChatOpsToolRounds)

// chatOpsCapSignalNote / chatOpsCapTimeoutNote are the two clauses the round-cap
// error carries when the turn ended for a reason OTHER than the model looping.
// The user reads this one line to decide what to do next, so "the model would
// not stop" and "you interrupted it" must not be confused (Rule 15: a failure
// names its cause).
const (
	chatOpsCapSignalNote  = " (you interrupted the turn — the session is still open; send it again, /clear to reset, or /quit to exit)"
	chatOpsCapTimeoutNote = " (the turn hit its time budget)"
)

// chatOpsCapAdvice is the tail every round-cap error carries: the state the
// session is left in and the two ways out. Without it the failure line read
// "chat turn exceeded 40 tool rounds without a final answer" and the process
// then DIED — the operator had no way to tell a dead REPL from a live one.
const chatOpsCapAdvice = " — the session is still open; rephrase, /clear to reset, or /quit to exit"

// chatOpsCapNote renders the clause the round-cap error appends when the run ctx
// ended for an external reason. The cap is only reached while the ctx is NOT
// done (the round boundary returns on ctx.Done()), so an empty note means the
// cap really was the model looping.
func (a *Agent) chatOpsCapNote(ctx context.Context) string {
	if ctx == nil || ctx.Err() == nil {
		return ""
	}
	if a.interruptedBySignal() {
		return chatOpsCapSignalNote
	}
	return chatOpsCapTimeoutNote
}

// ToolEventKind distinguishes the two halves of one tool activity the
// OnToolEvents callback reports: KindCall fires before execution,
// KindResult fires after.
type ToolEventKind string

const (
	// ToolEventKindCall is emitted just before a tool call executes.
	ToolEventKindCall ToolEventKind = "call"
	// ToolEventKindResult is emitted just after a tool result is available.
	ToolEventKindResult ToolEventKind = "result"
)

// ToolEvent is one tool-activity record streamed through Agent.OnToolEvents
// (the web SSE `event: tool` frames). Kind distinguishes call vs result; Name
// is the tool ("bash", "read", …); Summary is a short one-line human-readable
// digest — the first non-empty line of the tool's input arguments for calls,
// the first line of the (possibly truncated) result for results.
//
// COMMAND IS THE UNCUT HALF OF THE SAME PARSE, and it exists because Summary is
// a DIGEST: firstLineOf caps a summary at toolCallSummaryMaxChars, which is
// correct for a glance and wrong for a paste — a 200-cell bash command arrives
// in Summary cut at 160 with a `…` in it. A terminal row whose whole value is
// "select the command and run it" therefore cannot be built out of Summary
// alone, and widening Summary would change a WIRE value the web transcript
// replays byte for byte (ToolCallSummary's doc, and Rule 5 of this epic). So the
// uncut bytes ride beside it: Command is the SAME argument value Summary was
// read from (toolCallDigestArg — one selection, two renderings), with no cut and
// no ellipsis. It is EMPTY for a tool whose chosen argument is not a command (a
// `read` call's `path`) and for every result event, and a consumer that needs a
// command falls back to Summary — that fallback keeps every existing surface
// byte-identical.
type ToolEvent struct {
	Kind    ToolEventKind `json:"kind"`
	Name    string        `json:"name"`
	Summary string        `json:"summary"`
	// Command is the call's command argument, uncut (see the type comment).
	// `omitempty` is correct here and only here: a call with no command has no
	// command to send, and its absence is the semantic "fall back to Summary"
	// rather than a lost value (Rule 15a).
	Command string `json:"command,omitempty"`
}

// ToolCallEvent is the CALL event for one tool call — the ONE constructor both
// the live wire and any renderer that needs to reproduce it use, so the pair
// (Summary digest, uncut Command) is stamped in exactly one place.
//
// WHY IT IS EXPORTED. cmd/agent's tool row is a pure renderer of this event, and
// its regression test must drive the REAL call path (`>200`-cell command →
// digest → row → copy text) rather than assemble an event by hand: an event a
// test built itself would prove only that the renderer agrees with the test's own
// idea of the wire. The constructor is the seam that makes that end-to-end
// assertion possible without exporting emitToolEvents (which calls a callback).
func ToolCallEvent(tc deepseek.ToolCall) ToolEvent {
	return ToolEvent{
		Kind:    ToolEventKindCall,
		Name:    tc.Name,
		Summary: ToolCallSummary(tc),
		Command: toolCallCommand(tc),
	}
}

// emitToolEvents fires the optional OnToolEvents callback once per call and
// once per result (a call whose callback is nil still executes — the callback
// is pure display). The returned []string of result summaries is consumed by
// ChatOps to emit the matching result events after execution.
func (a *Agent) emitToolEvents(toolCalls []deepseek.ToolCall) []string {
	summaries := make([]string, len(toolCalls))
	if a.OnToolEvents == nil {
		return summaries
	}
	for j, tc := range toolCalls {
		ev := ToolCallEvent(tc)
		summaries[j] = ev.Summary
		a.OnToolEvents(ev)
	}
	return summaries
}

// emitToolEventResults fires OnToolEvents with the first line of each result
// string (positions aligned with toolCalls). No-op without the callback.
func (a *Agent) emitToolEventResults(toolCalls []deepseek.ToolCall, results []string, summaries []string) {
	if a.OnToolEvents == nil {
		return
	}
	for j, tc := range toolCalls {
		a.OnToolEvents(ToolEvent{Kind: ToolEventKindResult, Name: tc.Name, Summary: firstLineOf(summaries[j], results[j])})
	}
}

// toolResultHeaderMarker is the harness-reserved token the loop stamps onto
// every tool result it feeds the model ("<tick> [tool_use id: X]\n<payload>",
// loop.go's stamped result). A digest skips any line carrying it: the header
// names the CALL, while a digest must say what the tool returned. Named here
// (Rule 1b) because the live `event: tool` frame and the web transcript replay
// (B5) both depend on the same rule.
const toolResultHeaderMarker = "[tool_use id:"

// toolResultLines splits a tool result into the lines a digest may use: blank
// lines dropped, and the harness metadata line dropped (see
// toolResultHeaderMarker) — so a digest starts at the tool's own output.
func toolResultLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.Contains(ln, toolResultHeaderMarker) {
			continue
		}
		out = append(out, ln)
	}
	return out
}

// ToolResultDigest renders a one-line digest of a tool result: its first
// digestable line, capped at maxRunes runes (maxRunes <= 0 = uncapped; runes,
// not bytes, so a CJK result is never cut mid-character). Exported because the
// digest is a WIRE value — the live `event: tool` frame and a replayed
// /api/transcript tool row must say the same thing, so both call this one rule
// instead of each spelling their own.
func ToolResultDigest(raw string, maxRunes int) string {
	lines := toolResultLines(raw)
	if len(lines) == 0 {
		return ""
	}
	line := lines[0]
	if maxRunes > 0 {
		if r := []rune(line); len(r) > maxRunes {
			line = string(r[:maxRunes]) + digestEllipsis
		}
	}
	return line
}

// digestEllipsis marks a digest a cap cut (the live frames truncate too, so a
// replayed row is honest about being partial).
const digestEllipsis = "…"

// toolCallDigestKeys are the string arguments a call is summarised from, in
// priority order: the first one present is the call's digest AND the argument
// ToolEvent.Command may carry uncut (toolCallDigestArg). Declared once here so the
// two renderings cannot drift (Rule 1: a keyword list is a value, not a literal).
var toolCallDigestKeys = []string{"path", "file_path", "command", "cmd", "query", "url", "pattern", "symbol", "message", "content"}

// ToolCallSummary renders a one-line digest of a tool call's input: the tool
// name is carried separately, so this is the first meaningful argument (a path,
// a command, a query…) — exactly what a human wants to see at a glance in a
// collapsed tool card. Exported because the digest is a wire value, not an
// internal detail: the web transcript replay (/api/transcript, B5) must render
// the SAME string the live `event: tool` frame carried, and a second
// implementation would be free to drift from it.
func ToolCallSummary(tc deepseek.ToolCall) string {
	if arg, ok := toolCallDigestArg(tc); ok {
		return firstLineOf("", arg)
	}
	// Fall back to the raw arguments (compact), then a stable placeholder.
	if len(tc.Arguments) > 0 {
		if b, err := json.Marshal(tc.Arguments); err == nil && len(b) > 0 {
			s := string(b)
			if len(s) > toolCallSummaryRawMaxChars {
				s = s[:toolCallSummaryRawMaxChars] + digestEllipsis
			}
			return s
		}
	}
	return "(no arguments)"
}

// toolCallSummaryMaxChars caps the summary a call renders (firstLineOf's cut).
// Named here because the value is a CONTRACT rather than a taste: a 200-cell
// command comes back cut at this many cells with a `…`, which is the defect
// ToolEvent.Command exists to answer — so the cap and the uncut field have to be
// read together. (Runes, not bytes: firstLineOf counts bytes today and its cut is
// re-implemented nowhere else, so this constant documents the site rather than
// moving it.)
const toolCallSummaryMaxChars = 160

// toolCallSummaryRawMaxChars caps the compact JSON fallback ToolCallSummary uses
// when a call carries no digestable string argument.
const toolCallSummaryRawMaxChars = 120

// toolCallDigestArg is the ONE selection rule behind both halves of a call's
// rendering: the argument ToolCallSummary digests (in toolCallDigestKeys order)
// and the argument ToolEvent.Command carries UNCUT. It returns the chosen string
// argument and whether one was found.
//
// WHY ONE FUNCTION AND NOT TWO LOOPS. The two renderings must never disagree
// about WHICH argument they describe — a Summary reading `command` beside a
// Command carrying `path` would put a path in a shell prompt while the row above
// it showed the command it was digesting. Asking here means the pair is derived
// from one selection, so a later key added to the list moves both halves.
func toolCallDigestArg(tc deepseek.ToolCall) (string, bool) {
	for _, key := range toolCallDigestKeys {
		if v, ok := tc.Arguments[key].(string); ok && v != "" {
			return v, true
		}
	}
	return "", false
}

// toolCallCommandArg is the key ToolEvent.Command is read from: the arguments a
// shell command actually arrives as. Both spellings are here because the tool
// surface uses both (bash's `command`, and the `cmd` alias some definitions
// carry) — the same two keys toolCallDigestKeys lists.
var toolCallCommandKeys = []string{"command", "cmd"}

// toolCallCommand returns the call's command, UNCUT, for ToolEvent.Command: the
// chosen argument IF it is one of toolCallCommandKeys. A `read` call's path is a
// digest, not a command, so it yields "" and the renderer falls back to Summary —
// which is what keeps every existing surface byte-identical.
func toolCallCommand(tc deepseek.ToolCall) string {
	for _, key := range toolCallCommandKeys {
		if v, ok := tc.Arguments[key].(string); ok && v != "" {
			return v
		}
	}
	return ""
}

// firstLineOf returns the first DIGESTABLE line of s (capped at 160 chars);
// when s carries no usable text it falls back to fallback.
func firstLineOf(fallback, s string) string {
	for _, ln := range toolResultLines(s) {
		if len(ln) > 160 {
			ln = ln[:160] + digestEllipsis
		}
		return ln
	}
	if fallback != "" {
		return fallback
	}
	return "(empty result)"
}

// chatOpsToolDefs are the tool definitions the CHAT wire carries: the ordinary
// surface plus, when there is a surface to read, the chat-scoped one.
//
// WHY THE CHAT WIRE GETS ITS OWN LIST, AND THE TASK PATH DOES NOT. Rule 3 fixes
// claudeToolDefs as the SSOT of the ordinary tool surface — 17 tools, pinned
// against GOPI.md's inventory by TestGOPIRule3_MatchesClaudeToolDefs and
// demanded to carry a class for every entry by
// TestToolClassesCoverTheToolSurface. A tool appended THERE would be a new core
// tool, and it would also be advertised to every task run, which has no chat
// surface to read. So the chat-scoped definition lives in its own slice
// (chat_surface.go) and is appended HERE, at the ONE place the chat request is
// built: the task path's claudeToolDefs slice is untouched, and
// TestChatSurface_ToolDefOnlyOnTheChatWire asserts the two lists differ by
// exactly this one name.
//
// AND ONLY WHEN SOMETHING IS INSTALLED BEHIND IT. `chat_surface` reads the screen
// of the process the REPL runs in, and that process is the one that calls
// SetChatSurface. A carrier that runs the same ReAct loop without ever owning a
// surface (`gopi web --tools`, `gopi cli --tools`) therefore gets the byte-exact
// list it got before this tool existed — advertising a capability nothing can
// answer trades a sentence the model can act on for a call it can only lose.
//
// The slice is rebuilt per call rather than cached: the underlying array must
// never be handed to the wire twice (BuildClaudeRequest copies the slice before
// marking its last entry for prompt caching, but the discipline is to own our
// own array — a shared backing array is how a cache marker lands on a def
// nobody expected).
func (a *Agent) chatOpsToolDefs() []deepseek.ClaudeToolDef {
	extra := chatScopedToolDefs(a)
	defs := make([]deepseek.ClaudeToolDef, 0, len(claudeToolDefs)+len(extra))
	defs = append(defs, claudeToolDefs...)
	if a.chatSurface.surface != nil {
		defs = append(defs, extra...)
	}
	return defs
}

// RecordChatToolDefs publishes THIS chat wire's tool definitions to the context,
// so the context-size read-out counts what the server bills for them.
//
// It exists as its own entry point because the reading that was wrong is the one
// taken BEFORE the first request: a resumed session's status row paints from the
// context alone, and the request that would have told the context about its tools
// had not been built yet. The chat REPL therefore calls this once at start-up
// (cli_chat.go, beside setOpsFn) and ChatOps calls it again as the request is
// built — the later call matters because a chat surface installed after start-up
// ADDS a definition to the wire.
//
// A tool-less carrier (`Agent.Chat`, gopi web/cli without --tools) must not call
// it: it sends no tools, and a context that claimed some would over-report.
func (a *Agent) RecordChatToolDefs() {
	if a == nil || a.Ctx == nil {
		return
	}
	a.Ctx.SetTools(a.chatOpsToolDefs())
	// The chat wire's system prompt is COMPOSED per request (chatOpsSystemPrompt:
	// the tool-routing prompt + the chat-surface block + this run's transcript),
	// while Ctx.SystemPrompt keeps the plain base — it is also what WireMessages
	// renders, so overwriting it would change the wire rather than the estimate.
	// Recording the composed text separately is what closes the last gap between
	// the row and the bill: measured on a real request body, the base alone
	// reads 19k where the composed prompt plus tools plus messages is 26.8k
	// against the server's 27,358 (-2.2%).
	a.Ctx.SetEstimatePrompt(chatOpsSystemPrompt(a))
}

// SetEstimatePrompt records the system prompt the STATUS READING should use when
// it differs from the one the wire renders from Ctx.SystemPrompt. Estimate-only:
// it never reaches a request.
func (c *Context) SetEstimatePrompt(prompt string) {
	if c == nil || prompt == "" {
		return
	}
	c.estimateSystemPrompt = prompt
}

// chatScopedToolDefs returns the defs the CHAT wire carries in addition to
// claudeToolDefs — the ONE list that makes "a non-ordinary tool surface" a
// first-class notion rather than a name hard-coded in the chat path.
//
// It exists because the def↔dispatcher gate needs BOTH wires' vocabulary: a
// dispatch arm for a chat-scoped tool is correct, and a gate that only knows
// claudeToolDefs reads it as "the model was never told about this" (measured:
// TestGate_ToolDispatchCoversEveryDef failed on exactly that before this
// function existed). Keeping the extra defs behind a function — rather than
// appending chatSurfaceToolDef inline — is what lets the gate, the wire builder
// and the test all name the same set.
func chatScopedToolDefs(a *Agent) []deepseek.ClaudeToolDef {
	return []deepseek.ClaudeToolDef{chatSurfaceToolDefFor(a)}
}

// ChatOps is the tool-enabled chat turn — the "claude-code-style" half of
// chat mode: a free-form user request answered through a bounded ReAct loop
// with the full toolset (read/write/edit/bash/code_search/keyword_search/
// web_search/memory/…), so the user can ask for real operations ("fix this
// test", "what changed since yesterday?") conversationally.//
// It is deliberately NOT Agent.Run / Resume — a chat turn must never behave
// like a dispatched task. Differences from the ReAct task loop (runEngine):
//   - history seeds verbatim (multi-turn chat context), user input appended
//     verbatim — no [EPIC GOAL] framing, no task-list PLAN request;
//   - full tool-routing system prompt (the model must know the tools exist),
//     plus the project's GOPI.md as its own second system block (loaded ONCE at
//     session start by LoadProjectContext — the SAME reader and the SAME field
//     the task path uses; "" on a machine with no file above the cwd, which
//     BuildClaudeRequest drops, so such a run sends exactly what it sent
//     before). What a chat may NOT carry is the task SCAFFOLDING: no task-list
//     nudges / task-ledger registration / task-start memory enforcement /
//     task-list parsing;
//   - exit on the FIRST no-tool round that says anything at all (chat
//     answers are short; the bug022 "mid-task end_turn" double-confirm
//     pattern belongs to task runs). A one-shot continuation message covers
//     the degenerate "ended before saying anything" round — never a short
//     one, which has already been streamed to the carrier and would be
//     rendered twice.
//   - shared ReAct machinery reused: trimContext (compaction), job notices,
//     parallel tool batches, per-round log sync.
//
// Exit conditions: model ends without tool calls (answer returned), ctx
// cancelled (timeout), or maxChatOpsToolRounds exceeded (error).
//
// R5: images (non-empty = image turns) attach to the turn's user message —
// the same native image-block shape the task path uses (runEngine's first
// user message, loop.go) and the wire emits (claude_client.go user case,
// image block first, text after, never merged). Empty variadic = the plain
// text-only turn (identical to before).
// ChatOpsOnCarrier is ChatOps with the CARRIER named, so the chat write boundary
// judges this turn the way the face that asked for it actually works: on a face
// whose own banner promises "no approval gate" a `prompt` answer has no reader and
// degrades to the declared answer (internal/tools/chat_policy_carrier.go). The
// console form ChatOps keeps the console carrier verbatim.
func (a *Agent) ChatOpsOnCarrier(carrier, userInput string, history []deepseek.Message, images ...*deepseek.ClaudeImageSource) (result string, err error) {
	defer a.publishChatCarrier(carrier)()
	return a.ChatOps(userInput, history, images...)
}

func (a *Agent) ChatOps(userInput string, history []deepseek.Message, images ...*deepseek.ClaudeImageSource) (result string, err error) {
	startTime := time.Now()

	// Cost invariant — see Agent.Chat (the chats-table registration reads it).
	if a.Cost == nil {
		a.Cost = &deepseek.CostTracker{}
	}

	// Same stderr suppression as Agent.Chat: text surfaces once via OnText. The
	// turn's own message rides beside it for the same reason and with the same
	// clearing (the write boundary's authorization channel — see Agent.Chat).
	a.ChatMode = true
	defer func() { a.ChatMode = false }()
	defer a.publishChatTurn(userInput)()

	// Hippocampus in the tool-loop chat path: the scheduler's recalls derive
	// from the same cancel root as the run, and the level ceiling is clamped to
	// Lv-1 — this path HAS a round boundary (the tool loop below) but no task
	// flow to soft-interrupt, so Lv-3 is unreachable by rule, not by surprise
	// (devdoc/hippocampus.md §4.1). Nil host ⇒ both calls no-op.
	if a.Hippocampus != nil {
		a.Hippocampus.setBase(a.interruptRoot())
		a.Hippocampus.clampMaxLv(hippocampusLv1Append)
	}

	// Seed accumulated history verbatim, then append the user's turn verbatim
	// (no EPIC framing — this is a conversation, not a dispatch). The turn's
	// images ride in ONE user message: image blocks first, then the text (the
	// wire's user case builds exactly that order) — the same shape runEngine
	// and Agent.Chat use.
	a.Ctx.Messages = append([]deepseek.Message{}, history...)
	if len(images) > 0 {
		a.Ctx.AddMessage(deepseek.Message{Role: "user", Content: userInput, Images: derefImages(images)})
	} else {
		a.Ctx.AddUserMessage(userInput)
	}
	if a.SessionLog != nil {
		// CONSUME-ONCE, the same contract takePrefetchSeed states: the face is
		// published for THIS turn and cleared here, so a caller that publishes
		// nothing cannot have the previous turn's face inherited and stamp a
		// human line as self-typed (or the reverse).
		source := a.TurnSource
		a.TurnSource = ""
		// THE MOMENT IS CONSUMED ONCE, exactly like the face above: a caller
		// that published one (the banner painted it) gets that value in the
		// row, and a caller that published nothing gets the row minted at the
		// write — never the previous turn's moment, which would put a banner's
		// clock on a row it does not belong to.
		at := a.TurnOpenedAt
		a.TurnOpenedAt = time.Time{}
		a.SessionLog.LogUserFromAt(newUUIDv4(), userInput, source, at)
	}

	// The caller's prefetched retrieval enters as the model's OWN
	// memory(action=search) pair, immediately after the turn it answers — so the
	// hit lines carry record ids (#N), the feedback discipline covers them, and
	// the whitelist slim below can drop them once the model has had its round to
	// vote (chat_prefetch_seed.go states the design). The seed is CONSUMED here:
	// a turn whose caller published nothing seeds nothing, so no turn can
	// inherit another's retrieval.
	//
	// It is deliberately NOT written to the session JSONL. The prefetch's
	// injections have never ridden that file — the typing-time half records to
	// the session's own memory-prefetch.jsonl, and the text this used to append
	// to the message was logged no more than the message itself was — so the
	// adoption stays described where it happens: memory-prefetch.jsonl's
	// adoption line carries the query, the outcome and the section's size.
	a.seedPrefetchedRetrieval(a.takePrefetchSeed())

	ctx, cancel, endRun := a.runScopedContext(a.Timeout)
	defer endRun()
	defer cancel()
	var finalContent string
	// Per-turn reset of the checkpoint latches (on the host). ONE session log spans
	// every turn of a persistent chat (PersistChatSession), so a turn's
	// "already stored" must never leak into the next turn — otherwise turn 2
	// would silently lose its checkpoint. memoryStored is deliberately NOT
	// reset: it is the run-level store latch shared with the task loop, and a
	// persistent chat's earlier turn really did satisfy the store.
	a.resetChatOpsTurnCheckpoint()
	for round := 0; round < maxChatOpsToolRounds; round++ {
		// Hippocampus round stamp (nil-safe: no host, no stamp).
		a.stampHippocampusRound()
		// Round cap, made VISIBLE before it fires: the last
		// chatOpsRoundsWarnLead rounds inject a harness message telling the
		// model to wrap up, so a turn that would have been killed lands a
		// partial answer instead (and the operator sees it coming).
		if round >= chatOpsRoundsWarnAt {
			a.Ctx.AddUserMessage(chatOpsRoundsWarnMessage)
		}
		// Round boundary: persist buffered session/raw events (mirrors the
		// task loop — one fsync per round, not per event).
		if a.SessionLog != nil {
			a.SessionLog.Sync()
		}
		if a.RawLog != nil {
			a.RawLog.Sync()
		}

		// THE NOTE AS A SUBSTITUTE (turnfold.go): the chat surface's round
		// boundary, the same fold and the same POLICY as the task loop's. A chat
		// turn that ran ten tool rounds is ONE completed turn once the next input
		// arrives, and by this line that next input is in the context — so the
		// completed turn's raw traffic leaves HERE and its note takes its place.
		// No size threshold, no raw window unless GOPI_TURN_FOLD_KEEP_TURNS is
		// set; the turn in flight is never touched.
		a.foldCompletedTurns()

		// P0-3: background-job completion notices AND external messages enter
		// the inbox BEFORE the request is built — the model never polls.
		//
		// The external half first: `gopi notify --session <id> --message <text>`
		// delivered by ANOTHER process lands in this session's own inbox file,
		// and this is the ONE place that loads it into the in-process queue.
		// Order matters and is deliberate — the load MERGES into
		// theJobs.notices, so the take immediately below drains both kinds
		// through the single take point (there is no second queue and no
		// second injection).
		a.loadSessionInbox()
		for _, n := range a.noticesToInject(tools.TakeJobNotices()) {
			// Creation-time ground detection for the DETACHED half of a run's
			// work (the job tool's start action): the bash-round seam cannot
			// see a worktree/branch a background job created, so a row whose
			// ground comes from a job stayed repo-only. Same seam, same
			// writer, same ownership guards — only the caller differs (Rules
			// 13/14). Best-effort, exactly like the bash caller.
			tools.TaskDeliverableWorktreeJobForRow(a.autoTaskRowID, n.Command)
			a.injectJobNotice(n)
		}

		// Hippocampus round boundary — same channel as the task loop, Lv-1 max.
		a.roundBoundaryInjections(ctx)

		// Context window awareness: trim/compact before the request when the
		// budget is nearly exhausted (shared with the task loop).
		a.trimContext(ctx)

		// Per-response output cap: explicit Agent.MaxTokens wins, else the
		// package default.
		maxTokens := a.MaxTokens
		if maxTokens <= 0 {
			maxTokens = deepseek.DefaultClaudeMaxTokens
		}

		// Thinking pause/probe mirrors the task loop: a run whose thinking
		// keeps blowing the budget runs thinking-disabled, with one
		// thinking-enabled recovery probe every thinkingProbeSpacing rounds.
		thinking := a.Thinking
		wireEffort := a.ReasoningEffort
		paused, probe := a.thinkingPausedForRound()
		a.reportEffectiveThinkingBudget(thinkingBudgetOf(thinking), maxTokens, effectiveThinkingBudgetOf(thinking, maxTokens))
		if (paused && !probe) || thinking == nil || thinking.Type == thinkmode.Disabled {
			thinking = &deepseek.Thinking{Type: thinkmode.Disabled}
			wireEffort = ""
		}
		a.thinkingProbeRound = probe

		// The context's tool-cost read-out follows THIS wire: the definitions
		// below are what the server bills, so they are recorded before the
		// request is built (a surface installed after start-up adds one).
		a.RecordChatToolDefs()

		buildOpsRequest := func() *deepseek.ClaudeRequest {
			return deepseek.BuildClaudeRequest(
				a.Model,
				a.requestMessages(a.KeepThinking),
				// UNION of two independent chat-wire contributions that meet at
				// this call: master's def axis (a.chatOpsToolDefs() — the ordinary
				// surface plus the chat-scoped def) and the branch's prompt axis
				// (chatOpsSystemPrompt(a) — takes the Agent so the block can name
				// THIS run's own transcript). Different arguments, neither subsumes
				// the other, so BOTH are kept: the prompt arg is the branch's
				// Agent-carrying form (it already renders master's chat block plus
				// chatSessionBlock), the def arg is master's method (the branch's
				// pre-refactor claudeToolDefs would drop the chat-scoped def).
				chatOpsSystemPrompt(a), // FULL tool-routing prompt + the chat-surface block + this run's transcript
				a.Ctx.ProjectContext,   // the project's GOPI.md, when one was loaded — same block the task path sends
				a.chatOpsToolDefs(),    // ordinary surface + the chat-scoped def when a surface is installed
				maxTokens,
				thinking,
				wireEffort,
			)
		}

		// The SEND-TIME token gate (request_tokens_cap.go): the request is
		// counted on the exact BPE basis and refused when it does not fit, so a
		// giant tool_result assembled earlier in this round cannot be sent.
		res, err := a.chatOnceGated(ctx, buildOpsRequest, round)
		// PER-TURN INTERRUPT (task 01a0e4390): the OPERATOR abandoned this turn
		// with a run-scoped cancel, and the session stays open — report the
		// interruption as its own outcome so the carrier prints the marker and
		// the JSONL records the turn as interrupted. The witness is written by
		// the run (the only party that saw every delta).
		//
		// It is checked BEFORE the error, and unconditionally, because a
		// cancelled stream returns its partial text with a NIL error: the round
		// would otherwise continue as if the model had answered, one ReAct round
		// further into a turn the operator already abandoned.
		if a.TurnInterrupted(ctx) {
			a.recordTurnInterruption(InterruptSourceOperator)
			return "", errTurnInterrupted
		}
		if err != nil {
			// Signal-interrupt semantics (chat-once/bare-prompt zombie fix):
			// Interrupt() cancelled the ctx mid-turn (round-boundary or
			// in-flight stream) — surface the interrupt error so runChat's
			// turn loop unwinds to finishChat (session_end + CloseChatRow).
			//
			// AND RECORD IT (measured, session 07d98407). This arm used to
			// return without writing the witness the operator arm two lines
			// above writes, so a signal-ended turn printed "interrupt
			// received / ended (interrupt)" while session.jsonl carried ZERO
			// turn_interrupted rows — 1356 lines of human/notify and a raw.log
			// parked on one unanswered REQUEST. The screen and the record
			// disagreed, and only the record survives the terminal.
			//
			// The source is the SIGNAL's, never the operator's: no key was
			// pressed, and InterruptSourceOperator here would attribute a
			// supervisor's SIGTERM to the person at the keyboard. The reason
			// follows the same rule — SignalReason names the signal
			// (signalrec's spelling, carried by CloseSignal) instead of
			// TurnInterruptReason's ctrl-c default, which is only honest when a
			// key really was pressed.
			if a.interruptedBySignal() && ctx.Err() != nil {
				a.recordTurnInterruption(InterruptSourceSignal)
				return "", interruptErr
			}
			return "", err
		}
		content, toolCalls := res.content, res.toolCalls

		// IS THIS ROUND THE TURN'S ANSWER? Decided HERE — before the round's rows
		// are written — because a `request` row CLOSES the round it follows
		// (gopilog's reader stamps the content accumulated before the row with
		// that row's ordinal), so the answer's text has to be on disk BEFORE its
		// own round's row, not after the turn's last one.
		//
		// THE DEFECT THIS FIXES (measured on session d8943792, 2026-10-11, and
		// reproduced by TestChatRoundOrder_NoEmptyRound): the answer used to be
		// logged once at the END of the turn, i.e. AFTER the last round's
		// `request` row. Every turn therefore closed the NEXT row with its
		// answer, leaving its own last row contentless and moving the next
		// round's body one ordinal up — `gopi session log <id> --rounds` printed
		// that row as "(this round carried no content)" while it still carried a
		// live completion count (20 of 70 rows on that session, one per turn).
		//
		// It is the SAME order the task loop already uses (loop.go: assistant
		// content, then LogRequest), which is why the task path never showed it.
		acceptAnswer := len(toolCalls) == 0 && strings.TrimSpace(content) != ""

		// Session JSONL: tool_use blocks + request stats are recorded per ROUND
		// (each round really did happen on the wire), and the assistant's TEXT is
		// recorded exactly ONCE per turn — here, in the round that produced it,
		// so it lands inside its own round and not in the tick after it.
		//
		// WHY ONE TEXT ROW AND NOT ONE PER ROUND: a chat turn may run several
		// rounds (the degenerate-round guard re-issues a round whose answer is
		// shorter than minFinalAnswerChars), and one user message must produce
		// exactly ONE assistant answer record: logging every round's text
		// recorded a terse answer twice, which the TUI rendered back to back as
		// "gopi> pty-okpty-ok" (repro "Reply with exactly: pty-ok."). The only
		// round whose text IS the answer is the accepting one — a tool round's
		// text is working notes and keeps riding its tool_use rows.
		if a.SessionLog != nil {
			for _, tc := range toolCalls {
				a.SessionLog.LogAssistantToolUse(tc)
			}
			if acceptAnswer {
				a.SessionLog.LogAssistantText(content)
			}
			// RUNNING TOTALS, and the row SAYS SO (requestusage.FieldUsageBase).
			// The chat path's CostTracker holds the segment's running sum, and
			// that is the quantity this row records — a reader folds it by
			// taking the segment's last reading, never by adding the column
			// (epic 01a195b0: adding it made this session's headline the sum of
			// its own running totals). The session_end written by
			// CloseChatSession carries the same totals.
			p, h, m, comp := 0, 0, 0, 0
			if a.Cost != nil {
				p = a.Cost.TotalPromptTokens
				h = a.Cost.TotalCacheHitTokens
				m = a.Cost.TotalCacheMissTokens
				comp = a.Cost.TotalCompletionTokens
			}
			a.SessionLog.LogRequestCumulative(round, p, h, m, comp)
		}

		if len(toolCalls) == 0 {
			// No-tool round: whatever the model said here IS the turn's
			// answer — it was streamed to the carrier (TUI / browser / cli)
			// the moment the model produced it, and the carrier renders
			// exactly one answer per turn.
			//
			// So a non-empty round is ACCEPTED, however terse. The former
			// length test (>= minFinalAnswerChars) re-issued the round for a
			// short answer, which delivered the same text a second time —
			// measured with this very repro, "Reply with exactly: pty-ok.":
			// the answer streamed as "pty-okpty-ok" and the session JSONL
			// carried TWO assistant records for ONE user message. The
			// re-ask enforced nothing either (the second round's answer was
			// accepted at any length), so its only effect was the duplicate.
			//
			// The one-shot continuation survives for the genuinely degenerate
			// round: the model ended its turn having said NOTHING. Nothing was
			// streamed there, so asking again duplicates nothing.
			if strings.TrimSpace(content) != "" {
				// SELF-RECORDING for the tool-enabled chat path: the same ONE
				// capture the plain path does (loop.go), reached from the round
				// whose text is the turn's answer — the block the model wrote
				// must land in this session's record file, or "a chat records
				// its rounds" would be true of one chat and false of the other.
				a.captureSelfNotes(a.recordSelfNotes(content, res.thinking))
				// ...and the response object beside it (selfnotecaptureresponse.go):
				// the answer this chat displayed is where a table or a diagram
				// lives, and the note is capped at maxSelfRecordNoteChars.
				a.captureResponseText(content)
				a.Ctx.AddAssistantMessageWithThinking(content, nil, res.thinking, res.thinkingSig)
				finalContent = content
				break
			}
			if a.continuationShot {
				// Second silently-empty round in a row: accept it rather than
				// loop forever (the caller reports the empty turn).
				a.Ctx.AddAssistantMessageWithThinking(content, nil, res.thinking, res.thinkingSig)
				finalContent = content
				break
			}
			a.continuationShot = true
			a.Ctx.AddAssistantMessageWithThinking(content, nil, res.thinking, res.thinkingSig)
			a.Ctx.AddUserMessage(chatOpsEndTurnContinuation)
			continue
		}
		a.continuationShot = false
		// This round really did use tools, so the turn has (or is about to
		// have) something worth persisting — arms the per-turn checkpoint.
		a.markChatOpsToolRound()

		// Tool round: record the assistant turn, execute the batch, append
		// results with the same tick+tool_use_id stamping the task loop uses
		// (the model copies these ids for context_trim / memory_feedback).
		a.Ctx.AddAssistantMessageWithThinking(content, toolCalls, res.thinking, res.thinkingSig)
		// Tool activity surfaces to external carriers (the `gopi web` SSE
		// stream) right before and after execution — pure display, it never
		// affects the loop (no-op when OnToolEvents is nil, i.e. gopi chat).
		eventSummaries := a.emitToolEvents(toolCalls)
		results := a.executeToolBatchCtx(ctx, toolCalls)
		a.emitToolEventResults(toolCalls, results, eventSummaries)
		for j, tc := range toolCalls {
			id := tc.ID
			if id == "" {
				id = fmt.Sprintf("call_%d", j)
			}
			stamped := a.withTick(fmt.Sprintf("[tool_use id: %s]\n%s", id, results[j]))
			if a.SessionLog != nil {
				a.SessionLog.LogToolResult(id, stamped)
			}
			a.Ctx.AddToolResult(id, stamped)
		}
		// The checkpoint goes in AFTER the whole batch of tool results, never
		// between a tool_use and its result: the wire requires every tool_use
		// to be answered before anything else is appended, and inserting in
		// the middle would orphan them (the same slice reasoning
		// trimCommittedTaskArea relies on, loop.go).
		a.maybeChatOpsStoreCheckpoint()

		// The SAME whitelist context slim the task loop runs (loop.go's
		// runEngine), on the same terms — which is what makes a vote have a
		// consequence on this path. Every memory action=search pair whose records
		// were not voted useful is dropped from the message array, the prefetched
		// one included now that it IS a pair; the searches executed in THIS round
		// are exempted (currentMemorySearchIDs), so the model always gets one full
		// round to vote before a pair it just received can go.
		//
		// REUSED, NOT REIMPLEMENTED (Rule 35): the predicate, the record-id
		// association and the removal are the existing helpers, so the task path
		// and both chat surfaces cannot drift apart about what "unrewarded" means.
		//
		//
		// The SEEDED pair gets NO grace, and that is the point rather than an
		// omission: grace exists because a model decides its tool calls BEFORE
		// seeing their results, so a search it just issued needs one more round
		// to be votable. The seeded pair was in front of the model when it
		// composed THIS response — its result landed before the request that
		// produced these tool calls — so this response IS its vote opportunity,
		// exactly the one round a self-issued search gets. Adding it to the
		// grace set would hand the prefetch a deadline nothing else has and leave
		// a hit the model declined to vote on riding one round longer than a
		// self-issued one.
		a.whitelistContextSlim(currentMemorySearchIDs(toolCalls))
	}
	if finalContent == "" {
		return "", fmt.Errorf("chat turn exceeded %d tool rounds without a final answer%s%s",
			maxChatOpsToolRounds, a.chatOpsCapNote(ctx), chatOpsCapAdvice)
	}

	// Session JSONL: the turn's answer was already written by the round that
	// produced it (the LogAssistantText beside LogRequest above). It must sit
	// INSIDE its own round, before that round's `request` row — a row CLOSES
	// the content that precedes it (gopilog's reader stamps the accumulated
	// blocks with the ordinal of the row that follows them), so a text written
	// here, after the last round's row, would close the NEXT row instead and
	// leave this turn's own last row contentless.
	//
	// The count is still ONE per turn, which is the contract this call site used
	// to enforce by being the only one: the accepting round is the only round
	// whose text is the answer (a tool round's text is working notes and rides
	// its tool_use rows), so a multi-round turn still records exactly one
	// assistant answer record. Re-logging it here would both duplicate that
	// record (the `pty-okpty-ok` defect, devdoc/chat-duplicate-assistant-record.md
	// — a turn is re-asked for a short answer, and the carried text is streamed
	// to the carrier the moment the model produces it) and restore the very
	// misnumbering the move fixed.
	//
	// Multi-turn chats share ONE session log (PersistChatSession), so a fine
	// "which round logged this" seam exists if a future carrier ever wants it
	// — but the record count per turn is the contract.

	// Turn bookkeeping identical to Agent.Chat: session stays open in
	// persistent mode (single session id/log across turns — the caller
	// closes it via CloseChatSession), otherwise this turn is a clean
	// self-contained session. No ⏱ task-stats line, no task-ledger row.
	if a.PersistChatSession {
		if a.SessionLog != nil {
			a.SessionLog.Sync()
		}
		if a.RawLog != nil {
			a.RawLog.Sync()
		}
	} else {
		elapsed := time.Since(startTime)
		if a.RawLog != nil {
			a.RawLog.Close() //stylegate:ignore raw-log flush at exit; same best-effort audit channel as the session log
		}
		if a.SessionLog != nil {
			totalTokens := 0
			if a.Cost != nil {
				totalTokens = a.Cost.TotalPromptTokens + a.Cost.TotalCompletionTokens
			}
			a.SessionLog.LogSessionEnd(0, 1, totalTokens, elapsed.Seconds(), nil, a.endCauseFor(0))
			a.endOfRunHippocampus(ctx)
			a.SessionLog.Close() //stylegate:ignore session log flush at exit; the JSONL is append-only and best-effort
		}
	}

	return finalContent, nil
}
