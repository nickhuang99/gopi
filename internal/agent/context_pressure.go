package agent

// context_pressure.go — WHAT THE OPERATOR IS TOLD WHEN THE WINDOW FILLS UP.
//
// THE POLICY THIS FILE EXISTS FOR (operator's ruling, 2026-11-08): the automatic
// context reduction path (fold → compact → trim, loop.go's trimContext) no
// longer runs by default. A conversation that grows past the trigger level is
// NOT silently emptied any more; the operator is TOLD, with the numbers, and the
// choice (finish here, or start a new session and pull details back on demand)
// is theirs. That is why the message names three things and never fewer: the
// fraction used, the recommendation to start a new session, and the tool that
// gets detail back (session_search, which reads this run's own JSONL on disk).
//
// WHY A CALLBACK AND NOT A LOGGER LINE. Logger writes to stderr, and on a chat
// console stderr is either off-screen under the reserved layout or erased by the
// editor's next repaint (measured: cmd/agent/lineedit_wire.go's WriteNotice
// comment records the case — `/new` printed into the raw PTY stream and the
// screen showed a bare `you>`). A warning the operator cannot see is not a
// warning, so this file hands the text to Agent.OnUserNotice, which every carrier
// (chat / term / web) wires to its OWN announcement seam, and only then ALSO
// records it on the Logger for the session's console log.

import "fmt"

// AutoReduceEnv is the ONE switch that re-enables the automatic context
// reduction path (trimContext's fold → compact → trim) that used to run by
// default. It is truthy-ONLY: unset, empty, `off`, `0`, `false` and anything
// unrecognised all mean the policy this epic rules for — WARN, REMOVE NOTHING.
//
// WHY THE DEFAULT IS OFF AND THE SWITCH IS A NEW NAME. The old switch
// (GOPI_COMPACT, default ON) governed only the LLM summarizer; the two
// zero-LLM halves (the tool-result fold and the deterministic trim) ran
// regardless, so "compaction off" never meant "nothing is removed". A run that
// wants the old behaviour now says so once, with a name that describes what
// actually happens. Keeping the default ON would leave the reported defect —
// a conversation emptied behind the operator's back — in place for everyone who
// upgraded without reading a changelog (Rule 15: silence is the defect).
//
// GOPI_COMPACT still decides WHICH reduction runs once this switch is on:
// `GOPI_COMPACT` truthy = LLM summarizer; off/unset = fold + deterministic trim.
const AutoReduceEnv = "GOPI_CONTEXT_AUTOREDUCE"

const (
	// contextPressureNoticeStep is the percentage granularity of the notice: the
	// operator is told once per step crossed (75, then 85, then 95 …), never once
	// per round. A warning printed on every round of a full session is noise the
	// operator learns to skip — the same failure as printing nothing.
	contextPressureNoticeStep = 10

	// contextPressureNoticeFormat is the ONE statement of the condition. It is a
	// format string rather than a concatenation so the numbers and the advice
	// cannot drift apart between the carriers (they all render this one text via
	// contextPressureNotice).
	//
	// The percentages are rendered from the SAME pair of integers the trigger
	// judged, so a reader can check the claim against the numbers beside it.
	contextPressureNoticeFormat = "⚠️ context pressure: %d of %d tokens used (%.0f%% of the %d-token window) — automatic compaction is OFF (%s=1 re-enables it), so NOTHING was removed from this conversation. Finish what you need here, then START A NEW SESSION for the next task: this session keeps every message, so it will keep getting slower, and it will eventually reach the send-time cap. Details you still need are not lost — the session's own record is on disk and session_search pulls them back on demand."
)

// contextPressureNotice renders the operator-facing warning for a context that
// has crossed the trigger level. used/limit/window are the numbers the trigger
// already measured; nothing here re-measures (a second basis in a warning line
// is how two numbers in one sentence start disagreeing).
func contextPressureNotice(used, limit, window int) string {
	pct := 0.0
	if window > 0 {
		pct = float64(used) / float64(window) * 100
	}
	return fmt.Sprintf(contextPressureNoticeFormat, used, limit, pct, window, AutoReduceEnv)
}

// contextPressureBucket returns the step-bucket `pct` falls into, so the caller
// can tell "already told this level" from "crossed a new one".
func contextPressureBucket(pct float64) int {
	return int(pct) / contextPressureNoticeStep * contextPressureNoticeStep
}

// warnContextPressure is the WARN-ONLY half of trimContext, for the path that
// never reduces anything.
//
// WHY IT EXISTS: a plain `gopi chat` (no `--tools`) runs Agent.Chat, which does
// not call trimContext at all — so before this function a default chat session
// could fill its window, reach the send-time cap and die, WITHOUT ONE WORD to
// the operator. Measured on this tree 2026-11-08: with GOPI_CONTEXT_WINDOW=2000
// and a 3000-char prompt, `gopi chat --tools` printed the warning and
// `gopi chat` printed nothing (the same conversation, the same 46,643 counted
// tokens). A warning that only one of the two chat paths can produce is a
// warning the operator cannot rely on.
//
// It deliberately does NOT reduce, even when AutoReduceEnabled is on: the
// no-tools chat turn never reduced before (nothing in Chat called the trim
// machinery), and giving it a NEW mutation path is a behaviour change this rule
// change did not ask for. Warn here; reduce only where reducing already
// happened.
func (a *Agent) warnContextPressure() {
	if a == nil || a.Ctx == nil || a.ContextWindow <= 0 {
		return
	}
	triggerRatio := a.CompactTriggerRatio
	if triggerRatio <= 0 {
		triggerRatio = DefaultCompactTriggerRatio
	}
	limit := int(float64(a.ContextWindow) * triggerRatio)
	if used := a.Ctx.EstimatedTokens(); used > limit {
		a.notifyContextPressure(used, limit)
	}
}

// notifyContextPressure announces the warning ONCE PER BUCKET: it renders the
// text, hands it to the carrier's user-facing callback (when one is installed)
// and records it on the Logger as well, so the session's console log carries the
// same sentence the operator read.
//
// Rule 15: when NO carrier installed the callback the warning is still recorded
// — the Logger line names that fact — rather than silently succeeding. A run
// with no operator at all (`--once`, a scripted caller) is exactly the case
// where the log is the only reader, and its line must say which of the two
// channels it took.
func (a *Agent) notifyContextPressure(used, limit int) {
	if a == nil {
		return
	}
	pct := 0.0
	if a.ContextWindow > 0 {
		pct = float64(used) / float64(a.ContextWindow) * 100
	}
	bucket := contextPressureBucket(pct)
	if bucket <= a.contextPressureBucket {
		return // this level was already announced
	}
	a.contextPressureBucket = bucket
	text := contextPressureNotice(used, limit, a.ContextWindow)
	if a.OnUserNotice != nil {
		a.OnUserNotice(text + "\n")
		return
	}
	if a.Logger != nil {
		a.Logger.Printf("%s (no user-facing carrier installed this notice, so it is recorded here only)", text)
	}
}
