package agent

// chat_memory_timing.go — the THIRD chat-specific behaviour rule: WHEN this
// surface stores a lesson.
//
// WHY IT EXISTS (the measured gap). The two other chat behaviour rules landed
// earlier — the write boundary (chat_write_boundary.go, a conversation does not
// commit) and the surface/self-observation block (chat_context.go). The timing
// rule did not: the EVENT-DRIVEN store contract (eventDrivenStoreTriggers,
// storetriggers.go) reached exactly two surfaces, and NEITHER is a chat prompt —
// the harness-injected task-start checkpoint (loop.go) and the memory TOOL
// DESCRIPTION (loop.go), both of which describe a TASK run. So a chat turn read
// only the tool description's "USE after a task with a reusable lesson" half and
// inherited task semantics ("task end is the floor") for a surface that has no
// task end. The operator's instruction is the chat's trigger, and it was
// unstated.
//
// WHY THE TOOL DESCRIPTION IS NOT THE FIX. It is the shared, worker-facing
// surface (Rule 3: claudeToolDefs is the SSOT for the task path and the
// tool-enabled chat path alike). A rule about how a CONVERSATION behaves, folded
// into it, would tell every background task run that it works by conversation.
// The rule belongs in the chat-only block, which is what chatContextRulesFor
// assembles.
//
// THE TWO LIMBS ARE THE HONESTY REQUIREMENT (Rule 15; the precedent is
// chatNoToolsLimb/chatToolsLimb in chat_context.go, and the write boundary's own
// pair). The plain path carries NO tools on the wire: it cannot call the memory
// tool at all, so "store it NOW" would be an instruction it cannot act on — and
// this block's own header already names that failure mode as the worst kind,
// "a FALSE instruction is the worst kind, because it is followed". The plain
// limb therefore says what is true of it: the operator's instruction to remember
// is one you HAND OVER rather than obey, and you must not claim a memory was
// written.
//
// THE TOOL-ENABLED LIMB IS AFFIRMATIVE BECAUSE IT IS TRUE OF THAT PATH. The
// memory tool IS on its wire (chatOpsToolDefs = the ordinary surface), and the
// timing statement is TRUE of it on the evidence, not by hope: the harness
// carries a chat-only per-turn store checkpoint keyed on the SAME event
// (maybeChatOpsStoreCheckpoint, chat_ops.go — "this turn has done real work with
// tools and has not stored anything yet"), so the prompt sentence and the
// server-side nudge describe one contract. The checkpoint is bounded by
// CONSTRUCTION to at most one per turn, and it is a NUDGE, not the rule: a turn
// where the operator says 记住 and the model stores immediately disarms it (the
// memoryStored latch), so the prompt rule is the earlier and the finer trigger.
//
// RULE 1/13/14 — ONE DEFINITION, TWO READERS. The event list is
// eventDrivenStoreEventList verbatim, never a re-typed copy: it is the same
// definition the worker's two surfaces reach through eventDrivenStoreTriggers,
// so chat and worker cannot drift into telling different LLMs different trigger
// sets. What the chat block deliberately does NOT render is
// eventDrivenStoreTaskEndClause — the worker's "task end is the floor" framing,
// which is FALSE here: there is no task end on this surface to be a floor. That
// refusal is the whole reason the constant was split (see storetriggers.go). The
// action vocabulary is toolnames.ToolMemory/ToolMemoryActionStore, the tool name
// the model actually calls (Rule 13).
//
// RULE 3/35: no new tool, no new script, no resident process, no state file —
// the paragraph is composed at assembly time from constants that already exist.

import (
	"strings"

	"github.com/nickhuang99/gopi/internal/toolnames"
)

// chatMemoryTimingMarker opens the timing paragraph. It is the string the tests
// (and a reader of a raw log) key on when the question is "was this run told
// WHEN to store?" — separated from the body so PRESENCE is a one-substring
// question, independent of the limb that follows.
const chatMemoryTimingMarker = "[CHAT MEMORY TIMING — the operator's instruction is your trigger]"

// chatMemoryOwnScopeMarker opens the 2026-10-14 ruling — the AUTHORITY half of
// this paragraph, and the one that was MISSING until now.
//
// THE MEASURED GAP. `WHEN` was stated (this paragraph, 2026-10-08) and the
// FORBIDDEN QUESTION was stated (storeOnTheSpotClause, rendered into the
// KERNEL's tool-routing block — recorddiscipline.go) — but the kernel is not
// this surface's voice, and what it says is "never ask 要不要记下来" about a
// lesson that lands. Nothing anywhere said the WIDER thing the operator ruled on
// 2026-10-14: that memory work — store AND search AND feedback AND update — is
// gopi chat's OWN CATEGORY of work, done unconditionally, at any moment, with no
// permission sought at all. So the console could still treat a `memory` call as a
// favour to request rather than a duty to perform, which is exactly the defect
// the ruling names ("你如果没有这个意识，那就是 system prompt 的问题").
//
// It is a SEPARATE marker from chatMemoryTimingMarker because the two are
// different claims and a reader asks them separately: "was it told WHEN" vs "was
// it told it DOES NOT NEED PERMISSION". One substring each, so neither can hide
// behind the other's presence.
const chatMemoryOwnScopeMarker = "[CHAT MEMORY IS YOUR OWN SCOPE — unconditional, at any moment, NO permission]"

// chatMemoryOwnScopeRuling is the operator's ruling VERBATIM (2026-10-14, chat
// session 6eb5902f-0fe1-4dee-a461-07101f8ed655): a paraphrase of a ruling is a
// different rule, and this one is the whole authority for the paragraph — the
// same reason chatPADutyNoCodingDebug keeps its phrase unedited.
const chatMemoryOwnScopeRuling = "存经验教训是 gopi chat 自身的范畴，不需要请示，随时随地，无条件"

// chatMemoryOwnScopeActions names the FOUR actions the ruling covers, each read
// from internal/toolnames (Rule 1/13/14 — the vocabulary the model actually
// calls, and the one the tool schema validates against) and joined rather than
// typed as one string, so a rename on the tools side cannot leave this paragraph
// teaching a verb the tool rejects.
func chatMemoryOwnScopeActions() string {
	return strings.Join([]string{
		toolnames.ToolMemoryActionStore,
		toolnames.ToolMemoryActionSearch,
		toolnames.ToolMemoryActionFeedback,
		toolnames.ToolMemoryActionUpdate,
	}, " / ")
}

// The two tails of the authority clause, for the reason this file's other limbs
// exist: the plain path has no memory tool on the wire, so "call it in this turn
// and never ask" is an instruction it cannot act on and a claim it must not make.
// The AUTHORITY is identical on both paths — the MECHANISM is not.
const (
	chatMemoryOwnScopeTools = "call it in THIS turn and do not ask: storing, searching, voting (feedback) and " +
		"correcting (update) are ordinary work of this surface, not a request to put to the operator"

	chatMemoryOwnScopeNoTools = "say what is worth keeping and hand the operator the lesson — but NEVER claim a " +
		"memory was stored, and never present this as a permission the operator must grant"
)

// chatMemoryOwnScopeRule renders clause (0) of this paragraph: the AUTHORITY.
// It precedes the timing event list on purpose — a reader who does not know the
// work is theirs will not act on WHEN it fires.
//
// RULE 1/13/14: the only literals here are the ruling's own Chinese sentence and
// the words of the claim; every tool/action name comes from internal/toolnames.
func chatMemoryOwnScopeRule(toolsOn bool) string {
	limb := chatMemoryOwnScopeNoTools
	if toolsOn {
		limb = chatMemoryOwnScopeTools
	}
	return chatMemoryOwnScopeMarker + " — " + chatMemoryOwnScopeRuling + ". This is not a courtesy the operator " +
		"grants per call and not an operation that waits for a turn to end: `" + toolnames.ToolMemory + "(action=" +
		chatMemoryOwnScopeActions() + ")` belongs to what this surface DOES, and " + limb + ". Deleting the " +
		"question is the point: \"shall I keep this?\" is already the wrong beat, because the context can be " +
		"compacted at any moment — an unrecorded lesson is a LOST lesson, and a permission asked for it is a " +
		"window for that loss. "
}

// chatMemoryStoreAction is the verb the timing rule names —
// `memory(action=store)` — composed, never typed (Rule 1/13).
func chatMemoryStoreAction() string {
	return toolnames.ToolMemory + "(action=" + toolnames.ToolMemoryActionStore + ")"
}

// chatMemoryWorkerContrast is the sentence that keeps the two behaviour modes
// APART, and it is the reason the rule is stated on the chat side rather than
// only in the worker's surfaces: on this surface "at the end" is not a moment
// that exists. It is a separate constant because the tests assert the contrast
// itself — a paragraph that dropped it would still read as a plausible store
// trigger while silently reinstating the worker's timing.
const chatMemoryWorkerContrast = "That is NOT a task run's timing: there is no task end here to defer to."

// chatMemoryTimingRules renders the timing paragraph for one limb.
//
// The SHARED half is the event contract itself (the verbatim
// eventDrivenStoreEventList — the EVENTS only, never the worker's task-end
// clause) plus the worker contrast. The TAIL is the limb: what THIS path can do
// about it.
func chatMemoryTimingRules(toolsOn bool) string {
	var b strings.Builder
	b.WriteString(chatMemoryOwnScopeRule(toolsOn))
	b.WriteString(chatMemoryTimingMarker)
	b.WriteString(" The operator's instruction IS the trigger here, and it fires at once: ")
	b.WriteString(eventDrivenStoreEventList)
	b.WriteString(". ")
	b.WriteString(chatMemoryWorkerContrast)
	b.WriteString(" ")
	if toolsOn {
		b.WriteString(chatMemoryTimingToolsLimb)
		return b.String()
	}
	b.WriteString(chatMemoryTimingNoToolsLimb)
	return b.String()
}

// The two tails. The difference is not cosmetic: the plain path has no memory
// tool on the wire, so teaching it the tools-branch behaviour (call the tool) is
// the false instruction the block's limbs exist to avoid.
//
// They are vars rather than consts because they COMPOSE the action spelling
// (chatMemoryStoreAction) rather than typing it — Rule 1/13/14's point is that
// the value has one definition, and a Go const cannot call a function.
var (
	chatMemoryTimingToolsLimb = "This path HAS " + toolnames.ToolMemory + ": when the moment arrives, call " +
		chatMemoryStoreAction() + " as part of THIS turn — do not wait for the turn to be over, and tell the operator " +
		"what you kept. Never claim a memory you did not write."

	chatMemoryTimingNoToolsLimb = "This path carries NO tools: you cannot call " + chatMemoryStoreAction() +
		" yourself. Say what is worth keeping, hand the operator the lesson to save, and NEVER claim a memory " +
		"was stored."
)
