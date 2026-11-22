package agent

// context_compress.go — THE ONE COMPRESSION GOPI HAS.
//
// TWO RULES, AND NOTHING ELSE:
//
//  1. By default the history is APPEND-ONLY. A round appends its messages and
//     nothing rewrites, folds, stubs or summarizes what is already there.
//  2. When the estimate crosses DefaultCompactTriggerRatio of the window, ONE
//     compression runs: the OLDEST RUN of messages is deleted from the head,
//     down to DefaultCompactTargetRatio of the window, and the [IMPORTANT] notes
//     this run recorded are put back at the head as one carrier message. The
//     newest DefaultCompactKeepRecent SUBSTANTIVE messages are never touched —
//     harness injection and content-less turns do not spend a slot of that
//     floor, and a token ceiling (compactKeepRecentTokensCap) bounds it.
//
// There is no LLM in this path. No tool result is folded. No memory block is
// summarized. The notes are never compressed: they are re-rendered verbatim from
// the run's own note list (selfrecord.go), which is why deleting the messages
// that carried them loses nothing.
//
// WHY THE THREE-LAYER PIPELINE IS GONE (fold → compact → trim, plus the
// ONLY-WARN interlude that replaced it). Each layer needed its own trigger, its
// own parameter family (GOPI_COMPACT_TRIGGER/_TARGET/_KEEP_RECENT/
// _MAX_SUMMARY_TOKENS/_COOLDOWN/_REFERENCE_ONLY), its own failure cooldown and
// anti-thrash breaker, and its own log vocabulary — and the layers disagreed
// about what had happened: a fold could shrink the context without the
// compaction seeing it, a compaction reset the measurement a trim then judged the
// survivors by, and a summarizer failure left the run "compacting" while a trim
// did the actual work. One deletion of a PREFIX, followed by re-injecting the
// notes, has no such states: it either removed messages or it removed none, and
// the count it reports is the count it did.

import (
	"fmt"
	"strings"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// DefaultCompactTargetRatio is the waterline ONE compression aims at: the
// fraction of the window the history is cut back to when the trigger trips.
//
// IT IS NOT A KNOB. The two names below are constants because the arithmetic is
// a property of the product, not of a run: an operator who wants a different
// reading sets GOPI_CONTEXT_WINDOW (the window), never a ratio.
const DefaultCompactTargetRatio = 0.35

// CompactionAnnouncement is the operator-visible record that a compression
// happened. It is exported and is ONE constant (rather than a format string
// typed at both sites) because the chat status row's legend quotes this line —
// TestStatusRowSymbolsHaveAReaderVisibleDefinition matches the legend against
// it, and a reworded announcement fails that test instead of leaving the help
// quoting a line no run emits.
const CompactionAnnouncement = "memory compaction: removed %d oldest message(s) — the notes this run recorded are carried at the head"

// Compression reasons. Named because the SAME function serves both callers and
// the log line must say which one asked — "the window filled" and "the request
// did not fit" are different facts even when the answer is identical.
const (
	// compressReasonThreshold is the round boundary's trigger (the window
	// crossed DefaultCompactTriggerRatio).
	compressReasonThreshold = "window threshold"

	// compressReasonSendCap is the send-time gate's demand: the request did not
	// fit RequestTokensCap, so the context must be smaller before it is retried.
	compressReasonSendCap = "send-time cap"
)

// Memory-block markers. No code writes these any more (the LLM summarizer that
// produced them is deleted), and they are kept, together with the predicate
// below, because they are a READER: a resumed session, a replayed transcript and
// the compaction log of an older run all still carry blocks opened by these
// strings, and a reader that cannot name what it is looking at reports it as a
// user message the operator typed.
const (
	memorySummaryMarker       = "[MEMORY COMPACTION — EARLIER WORK — REFERENCE ONLY]"
	memorySummaryLegacyMarker = "[MEMORY COMPACTION — EARLIER WORK]"
)

// isMemorySummaryMessage reports whether the message is a compaction memory block
// (either the reference-only or the legacy marker) — a block produced by the
// summarizer that an older gopi version ran.
func isMemorySummaryMessage(m deepseek.Message) bool {
	return strings.HasPrefix(m.Content, memorySummaryMarker) ||
		strings.HasPrefix(m.Content, memorySummaryLegacyMarker)
}

// compactTriggerTokens is the size at which the ONE compression runs: the
// window times DefaultCompactTriggerRatio.
func (a *Agent) compactTriggerTokens() int {
	if a == nil || a.ContextWindow <= 0 {
		return 0
	}
	return int(float64(a.ContextWindow) * DefaultCompactTriggerRatio)
}

// compactTargetTokens is the waterline the compression cuts back to.
//
// IT IS A RATIO OF THE MOBILE PART, NOT OF THE WINDOW — and that distinction is
// the whole reason this function has a floor in it. The request always carries a
// FIXED cost that no compression can remove: the system prompt, the project
// context (it rides inside the system prompt on the wire) and the tool
// definitions. At this project's own sizes that is ~110K tokens, so on a 200K
// window a plain `0.35 × window` = 70K target is BELOW the floor and unreachable
// by any deletion. Compressing toward it removes every deletable message, misses
// the waterline anyway, and re-triggers on the very next round — the "we cut
// everything and the count did not move" loop. So the target is
// `floor + (window - floor) × ratio`: the same fraction of what is actually
// available. When the floor has already outgrown the window the honest answer is
// the floor itself, and compressContext reports that it cannot help instead of
// deleting messages that buy nothing.
//
// The floor is measured on the SAME basis as EstimatedTokens (the BPE count
// through the context's own cache). A floor on chars/4 against a target on BPE
// would be the mixed-basis defect this package has already been bitten by twice.
func (a *Agent) compactTargetTokens() int {
	if a == nil || a.ContextWindow <= 0 {
		return 0
	}
	floor := a.fixedFloorTokens()
	if floor >= a.ContextWindow {
		return floor // nothing is mobile: the caller reports it, deletes nothing
	}
	target := floor + int(float64(a.ContextWindow-floor)*DefaultCompactTargetRatio)
	if limit := a.compactTriggerTokens(); target >= limit && limit > floor {
		target = floor + (limit-floor)/2
	}
	return target
}

// fixedFloorTokens is the immovable part of the request: the system prompt, the
// project context and the tool definitions, on the heuristic basis this package
// compares across a mutation (the same basis as heuristicTokens — a floor
// measured against a target on a different basis is mixed arithmetic).
//
// It does NOT include the keep-recent messages: that is a POLICY limit on what a
// compression may remove, not a cost the request carries whether or not anyone
// compresses. Folding it in would turn the recent tail into a hidden switch
// deciding whether compressing is possible at all.
func (a *Agent) fixedFloorTokens() int {
	if a == nil || a.Ctx == nil {
		return 0
	}
	return a.Ctx.fixedFloorTokens()
}

// fixedFloorTokens is the Context-level reading, so a caller holding only the
// context (a test, a status row) asks the same question.
func (c *Context) fixedFloorTokens() int {
	if c == nil {
		return 0
	}
	return c.textTokens(c.SystemPrompt) + c.textTokens(c.ProjectContext) + c.ToolsTokens()
}

// compressContext is THE compression, and the only place the history is ever
// shortened. Both callers reach it:
//
//   - trimContext (the round boundary) with compressReasonThreshold: it decides
//     WHETHER the window is full, then calls here.
//   - shrinkForRequestCap (the send-time gate) with compressReasonSendCap: the
//     request was counted over RequestTokensCap, so the context is compressed
//     before the retry.
//
// It returns the number of messages removed (0 = nothing changed), which is what
// the send-time gate's retry loop needs and what the threshold log reports.
//
// THE ORDER IS LOAD-BEARING: delete, then re-inject the notes. The notes' own
// carrier may have been among the deleted messages, so the re-injection must run
// AFTER the deletion — a carrier placed before it would be deleted with the head.
//
// measured is the send-time gate's own reading of the request that is about to
// be retried — the EXACT prompt-token count, taken with the BPE vocabulary
// through the V4.1 template, with the tools array included (request_tokens_cap.go).
//
// IT IS PASSED IN, NOT RE-DERIVED, and that is the whole point of the parameter:
// the trigger path has no such reading (it judges EstimatedTokens, a different
// arithmetic) so it passes nil, while the send-cap path hands over the number it
// just measured and was just refused on. A notice that reported the estimate on
// the send-cap path printed a number 3.2x smaller than the one that stopped the
// send (measured: counted=9794 cap=9000 beside "3033 of 12000 tokens used"), so
// the operator read a conversation that fitted and a run that refused to send it.
func (a *Agent) compressContext(reason string, measured *RequestTokenCount) int {
	if a == nil || a.Ctx == nil || a.ContextWindow <= 0 {
		return 0
	}
	before := a.Ctx.EstimatedTokens()
	limit := a.compactTriggerTokens()
	if reason == compressReasonThreshold && before <= limit {
		return 0 // appending is the default; a window that fits is never touched
	}
	removed, blocks := a.Ctx.dropOldestTo(a.compactTargetTokens(), DefaultCompactKeepRecent)
	if removed == 0 {
		// NOTHING TO REMOVE. Either the deletable part is already inside the recent
		// tail, or the request's FIXED cost (the system prompt: this project's own
		// rules are ~150 KB) alone fills the window — in both cases a compression
		// cannot help, and saying "compacted" here would be a lie that also inflates
		// the count. The operator is told instead, once, with the reason that applies
		// and with the number the refusal was actually decided on.
		a.notifyUncompressible(before, limit, a.Ctx.deletableMessages(DefaultCompactKeepRecent), measured)
		return 0
	}
	carried := a.ensureSelfRecordPresent()
	after := a.Ctx.EstimatedTokens()
	a.CompactCount++
	if a.Logger != nil {
		a.Logger.Print("🧠 " + fmt.Sprintf(CompactionAnnouncement+" (%s: est %d → %d tokens, %.0f%% → %.0f%% of the %d-token window, %d older memory block(s) among them, notes re-carried: %v)",
			removed, reason, before, after,
			float64(before)/float64(a.ContextWindow)*100, float64(after)/float64(a.ContextWindow)*100,
			a.ContextWindow, blocks, carried))
	}
	a.notifyCompaction(removed, before, after)
	return removed
}

// ContextWindowEnv names the window knob the operator-facing advice tells a
// reader to raise. It is a constant (Rule 1/13) because the notice RECOMMENDS it:
// a recommendation that spells the variable name inline is a second source of
// truth for the knob's spelling, and a renamed knob would leave the advice
// pointing at nothing.
const ContextWindowEnv = "GOPI_CONTEXT_WINDOW"

// uncompressedNoticeFormat is the statement for a compression that could NOT
// shorten the conversation while the request still fits the SEND ceiling: every
// message left is inside the recent tail, so this pass had nothing it may delete.
//
// THE NUMBER IT PRINTS IS LABELLED AN ESTIMATE, because on this path that is what
// it is — no send-time reading exists here (the trigger judges EstimatedTokens,
// see the notice's measured counterpart below). Presenting an estimate as a
// measurement is the defect this file was repaired for: the two lines an operator
// reads at one instant must never disagree about the same request.
//
// THE TAIL IS DESCRIBED THE WAY IT IS MEASURED — "substantive messages" and the
// token ceiling — because a notice that said "outside the newest 8" would quote
// the OLD unit at the operator and describe a floor this build no longer has.
const uncompressedNoticeFormat = "⚠️ context compression could not shorten this conversation: the ESTIMATE is %d of the %d-token window (limit %d) — an estimate, because the window trigger carries no send-time reading — and only %d message(s) are outside the protected tail (%d substantive messages, at most %d tokens), so there is nothing this pass may delete without breaking the round in flight. This request still fits the send ceiling, but the conversation will keep growing until this session ends: finish what you need here and START A NEW SESSION for the next task; this session's own record is on disk, so nothing already written is lost."

// unrecoverableNoticeFormat is the OTHER state, and it is a different statement
// rather than the same one with a number swapped: the send-time gate counted the
// request, found it over the ceiling, asked for a compression, and the compression
// had nothing to delete. Every later round is refused the same way, so "this will
// keep growing" understates it — nothing this session does can make the request
// sendable again, and the operator needs the levers, not a suggestion to finish up.
//
// IT CARRIES THE MEASURED COUNT. %d is RequestTokenCount.Counted — the exact BPE
// count of the request that was just refused, tools included — never
// EstimatedTokens: the two differ by 3.2x on a real session (measured
// counted=9794 cap=9000 beside an estimate of 3033), and a notice quoting the
// smaller number beside a refusal quoting the larger one reads as a bug in
// whichever line the reader trusts.
const unrecoverableNoticeFormat = "⚠️ context compression could not shorten this conversation, and the request is OVER THE SEND-TIME CEILING — counted=%d tokens against cap=%d. That count is the EXACT reading of the request that was just refused (system prompt, project context, tool definitions and every message, through the BPE vocabulary — not the chars-based estimate the window trigger uses), and only %d message(s) are outside the protected tail, so this pass had nothing it may delete. The state does not recover by itself: every later round re-counts the same request, is refused the same way, and START A NEW SESSION is the only step a compression can reach. The levers, cheapest first: (1) trim the project rules — the fixed cost is %d tokens and no compression may remove it; (2) raise the send-time ceiling for this run, %s (in force: %d); (3) raise the context window, %s (in force: %d), which is what the compression cuts against; (4) start a fresh session for the next task — this session's own record is on disk, so nothing already written is lost."

// fixedCostSentenceFormat names the FIXED cost when it alone has outgrown the
// window's compression limit: no conversation size can bring the request under
// it, so the advice names the inputs the operator can actually change.
const fixedCostSentenceFormat = " The request's FIXED cost alone — the system prompt, the project context and the tool definitions — is %d tokens, at or above the %d-token limit, so no conversation size can bring the request under it: trim the project rules, or raise %s."

// notifyUncompressible reports a compression that had no room to work, and it
// distinguishes the TWO states an operator must be able to tell apart with one
// glance:
//
//   - measured != nil && measured.Over — the send-time gate REFUSED this exact
//     request and nothing can be deleted, so the session is stuck: report the
//     measured count and the levers (unrecoverableNoticeFormat).
//   - otherwise — the window is merely full to the tail: report the estimate,
//     labelled as one, and the ordinary "finish up, start a new session" advice.
//
// A third fact rides either message: when the FIXED cost alone has outgrown the
// limit, no compression at any conversation size helps, and that sentence names
// it (fixedCostSentenceFormat).
func (a *Agent) notifyUncompressible(before, limit, deletable int, measured *RequestTokenCount) {
	text := ""
	if measured != nil && measured.Over {
		text = fmt.Sprintf(unrecoverableNoticeFormat,
			measured.Counted, measured.Cap, deletable,
			a.fixedFloorTokens(),
			RequestTokenCapEnv, measured.Cap,
			ContextWindowEnv, a.ContextWindow)
	} else {
		text = fmt.Sprintf(uncompressedNoticeFormat,
			before, a.ContextWindow, limit, deletable, DefaultCompactKeepRecent, compactKeepRecentTokensCap)
	}
	// THE FIXED-COST SENTENCE IS APPENDED ONLY WHERE IT IS NOT ALREADY SAID. The
	// unrecoverable text names the fixed cost as lever (1), so appending it again
	// printed the same paragraph twice in one notice (measured on a real chat run
	// before this guard: "The request's FIXED cost alone — … 39559 tokens …" twice).
	if floor := a.fixedFloorTokens(); floor >= limit && !(measured != nil && measured.Over) {
		text += fmt.Sprintf(fixedCostSentenceFormat, floor, limit, ContextWindowEnv)
	}
	if a.OnUserNotice != nil {
		a.OnUserNotice(text + "\n")
		return
	}
	if a.Logger != nil {
		a.Logger.Print("ℹ️ no carrier installed for the operator notice; it is logged here instead: " + text)
	}
}

// compactionNoticeFormat is the statement addressed to the OPERATOR when a
// compression runs. It exists because a compression is the ONE moment the
// conversation changes shape behind the reader: the numbers, what was removed,
// and where the run's own notes went all have to be visible, or the operator
// meets a shorter transcript with no explanation.
const compactionNoticeFormat = "⚠️ context compression: removed the %d oldest message(s) (%d → %d tokens) because the conversation crossed %d of the %d-token window. Nothing you recorded is lost: this run's [IMPORTANT] notes are carried at the head, and the deleted messages are still on disk in this session's transcript."

// dropOldestTo deletes the OLDEST RUN of messages until the estimate is at or
// below targetTokens, and returns how many messages went and how many of them
// were compaction memory blocks left by an older gopi version.
//
// THE UNIT OF DELETION IS A RUN, not a message: an assistant message and the
// tool results that immediately follow it leave together, so the surviving head
// can never be a tool result whose tool_use was removed — the endpoint rejects
// that (HTTP 400 "tool_use ids were found without tool_result blocks
// immediately after"). verifyToolUsePairs is the proof of the same invariant at
// the send boundary; this function is what keeps the invariant true here.
//
// THE HISTORY IS NEVER EMPTIED. A compression trades old context for budget; it
// must never trade away the request itself. Two guards, both load-bearing and
// both measured (2026-09-30, dumps/20260930-194502-1.dump.json): the recent tail
// (recentTailStart below) is never removed, and a run that would consume the
// whole history is refused. An over-budget request is strictly better than a
// malformed one — an empty slice serializes as `"messages":null` and the
// endpoint answers HTTP 422.
//
// THE TAIL IS MEASURED IN SUBSTANTIVE MESSAGES, AND BOUNDED IN TOKENS (fixed
// 2026-11-21). The COUNT decides how many messages the tail protects
// (recentTailStart), and the loop below may not leave fewer than that many — so
// the boundary is computed ONCE, before the loop: a predicate evaluated per
// iteration would make the loop quadratic on exactly the context that is already
// full, and it would also let the floor drift as the array shrinks.
//
// THE MEASUREMENT IS INVALIDATED when anything is removed: PromptTokens was
// taken over the OLD, larger range, so keeping it would score the survivors
// against a count that included the deleted messages, and the next round would
// re-trigger on the stale count.
func (c *Context) dropOldestTo(targetTokens, keepRecent int) (removed, blocks int) {
	if c == nil || targetTokens <= 0 {
		return 0, 0
	}
	if keepRecent < 0 {
		keepRecent = 0
	}
	// The ONE boundary, shared with deletableMessages (and therefore with the
	// refusal the operator reads): "how many messages may this pass remove" and
	// "how many did the loop stop short of" are the same number by construction,
	// never two arithmetics that have to agree.
	//
	// PROTECTED IS A COUNT, NOT AN INDEX: the index moves with every drop, the
	// count does not. Keeping the index here let a multi-iteration pass walk past
	// its own floor and consume the array (the `dropOldestTo(1, 1)` arm of
	// TestCompressionLoopCostIsLinear is what caught it), and an emptied history
	// serializes as `"messages":null`, which the endpoint answers HTTP 422.
	protected := len(c.Messages) - recentTailStart(c, keepRecent, compactKeepRecentTokensCap)
	// THE LOOP IS LINEAR, and that is a measured requirement, not a style choice
	// (bpe_basis_test.go's TestCompressionLoopCostIsLinear): asking
	// EstimatedTokens() per iteration re-prices the whole history every time —
	// n hashes and n map lookups per drop, on a context that is large BY
	// DEFINITION, since this is the path that runs when the window is full. With
	// the per-message BPE costs cached, the exact new total is one subtraction.
	est := c.EstimatedTokens()
	for protected > 0 && len(c.Messages) > protected && est > targetTokens {
		drop := 1 + pairedToolResultRun(c.Messages, 0)
		if drop > len(c.Messages)-protected {
			break // never empty the history, never cut into the recent tail
		}
		est = c.estimateAfterDroppingHead(drop, est)
		blocks += countMemorySummaryHead(c.Messages[:drop])
		c.Messages = c.Messages[drop:]
		removed += drop
	}
	// SAFETY NET: a head that is a lone tool result whose owner is already gone
	// (a resumed transcript can hold one) is dropped rather than sent. Bounded by
	// the same floor as the loop above.
	for protected > 0 && len(c.Messages) > protected && c.Messages[0].Role == "tool" {
		c.Messages = c.Messages[1:]
		removed++
	}
	if removed > 0 {
		c.PromptTokens = 0
		c.PromptTokensIndex = 0
	}
	return removed, blocks
}

// substantiveMessage is THE predicate the recent-tail boundary is counted on: a
// message a reader would recognise as content the model can work from.
//
// TWO CLASSES ARE NOT SUBSTANTIVE, and each is here because it was MEASURED
// spending a slot of the eight-message floor without buying anything (task
// 01a1fae1-411a: the eight newest messages of one real session held 19
// characters of real content beside 460 characters of harness injection):
//
//   - HARNESS INJECTION — a `[system] …` checkpoint, a task-area nudge, a
//     handle-forward block. It is addressed to the model, but it is the
//     FRAMEWORK talking, not the round: the same discipline that keeps it from
//     being attributed to the operator in a transcript (isHarnessInjectedUserMessage)
//     keeps it from claiming one of the eight protected slots.
//   - AN EMPTY TURN — no content, no tool call, no image, no reasoning. It is a
//     wire artifact (a streamed turn that produced nothing), and protecting it
//     protects a blank line.
//
// EVERYTHING ELSE IS SUBSTANTIVE, deliberately inclusive: a bare tool RESULT is
// substantive, because it is the material the next round is reasoning over, and
// so is a user message in ANY language or with any markup. The predicate is
// therefore a floor, never a filter — "is this worth keeping" is a judgement the
// compression may not make about the newest messages, and every heuristic that
// tried (length, prefix, role) would silently narrow the protection.
func substantiveMessage(m deepseek.Message) bool {
	if isHarnessInjectedUserMessage(m) {
		return false
	}
	return m.Content != "" || len(m.ToolCalls) > 0 || len(m.Images) > 0 || m.ReasoningContent != ""
}

// recentTailStart answers the ONE question the compression and its refusal both
// need: at which index does the PROTECTED tail begin — i.e. how many of the
// oldest messages may a pass remove?
//
// It walks BACKWARDS from the newest message and stops as soon as both halves of
// the floor are satisfied: at least keepRecent SUBSTANTIVE messages are inside
// the tail, and the tail costs no more than capTokens. The two halves pull in
// opposite directions on purpose — the count is the floor ("do not throw away
// the round in flight"), the token ceiling is the ceiling ("do not hold the
// window hostage to it"), and a tail of eight 40 KB tool results is exactly the
// case the count alone would make unbounded.
//
// THE RESULT IS ALWAYS <= len(msgs)-1 when the array is non-empty: a compression
// may never take the whole history, so the newest message is inside the
// protected tail whatever the ceiling says. capTokens <= 0 disables the ceiling
// (the count still applies); keepRecent <= 0 protects nothing by count, which is
// what the linearity arm asks for.
//
// COST: O(tail), one CACHED token lookup per message considered — never a BPE
// pass per iteration (messageTokens is content-keyed, and the tail is by
// definition the part that was just priced).
func recentTailStart(c *Context, keepRecent, capTokens int) int {
	if c == nil || len(c.Messages) == 0 {
		return 0
	}
	msgs := c.Messages
	tailStart := len(msgs) - 1 // the newest message is never removable
	if keepRecent <= 0 {
		return tailStart
	}
	// PHASE 1 — the FLOOR: walk back admitting every message (substantive or not)
	// until keepRecent SUBSTANTIVE ones are inside the tail, or the array runs
	// out. This is the half that changed units: a run of harness injection and
	// blank turns no longer eats the floor — it simply does not satisfy it, so
	// the tail keeps extending until it reaches real content.
	tokens, substantive := c.messageTokens(msgs[len(msgs)-1]), 0
	if substantiveMessage(msgs[len(msgs)-1]) {
		substantive = 1
	}
	for i := len(msgs) - 2; i >= 0 && substantive < keepRecent; i-- {
		tokens += c.messageTokens(msgs[i])
		if substantiveMessage(msgs[i]) {
			substantive++
		}
		tailStart = i
	}
	// PHASE 2 — the CEILING: if that tail costs more than capTokens, give back
	// its OLDEST messages until it fits. This is the brake, not the rule: the
	// floor is what a pass normally keeps, and the ceiling only matters when the
	// substantive messages themselves are huge (a tail of eight 40 KB tool
	// results). When the two disagree the ceiling wins, because an unbounded
	// protected tail makes compression impossible by construction — the very
	// "nothing outside the newest N" refusal this file reports.
	if capTokens > 0 {
		for tokens > capTokens && tailStart < len(msgs)-1 {
			tokens -= c.messageTokens(msgs[tailStart])
			tailStart++
		}
	}
	return tailStart
}

// deletableMessages is how many messages a compression MAY remove: everything
// outside the recent tail. It is what a refusal has to report — "nothing left to
// delete" is a measurement, and without it the operator cannot tell "the window
// is tight" from "the project's own system prompt is the whole budget".
//
// IT ASKS recentTailStart, the same callee dropOldestTo uses (ONE judgement, not
// two) — including the token ceiling, so a refusal counts the tail the removal
// would actually have kept. Previously this subtracted keepRecent from the
// message count, which meant "deletable = 12" could be reported for a context in
// which the pass then removed 12 harness-injected messages and left 19
// characters of real content behind.
func (c *Context) deletableMessages(keepRecent int) int {
	if c == nil || keepRecent < 0 {
		return 0
	}
	return recentTailStart(c, keepRecent, compactKeepRecentTokensCap)
}

// countMemorySummaryHead counts compaction memory blocks inside a deleted run —
// only so the log can say how much of what went was a summarizer's output rather
// than the conversation itself.
func countMemorySummaryHead(msgs []deepseek.Message) int {
	n := 0
	for _, m := range msgs {
		if isMemorySummaryMessage(m) {
			n++
		}
	}
	return n
}

// notifyCompaction hands the compression statement to the carrier that can show
// it (Agent.OnUserNotice) and, when no carrier is installed, says so on the
// Logger rather than dropping it (Rule 15).
func (a *Agent) notifyCompaction(removed, before, after int) {
	text := fmt.Sprintf(compactionNoticeFormat, removed, before, after, a.compactTriggerTokens(), a.ContextWindow)
	if a.OnUserNotice != nil {
		a.OnUserNotice(text + "\n")
		return
	}
	if a.Logger != nil {
		a.Logger.Print("ℹ️ no carrier installed for the operator notice; it is logged here instead: " + text)
	}
}
