package main

// chat_replay_account.go — WHAT THIS RUN ACTUALLY SERVED, BESIDE WHAT THE
// RECORDING HOLDS.
//
// THE MEASURED DEFECT THIS FILE EXISTS FOR (2026-10-27, session 80c07a7d). A
// replay of a recording holding 152 model requests and 134 tool results served
// 93 requests and 82 tool results, and closed with this line:
//
//	[gopi replay] tool results served from the recording: 82 (unrecorded: 0),
//	model requests: 93 (script-exhausted: 0), turns typed: 11/11
//
// Every number on that line is true, and the line as a whole is a false
// completion claim: it never said what the RECORDING holds, so a run that served
// 61% of its script read exactly like a run that served all of it. "Typed 11/11"
// is a statement about the KEYBOARD, not about the script — the eleven prompts
// went in while 59 recorded responses were never asked for.
//
// So the completion statement is a COMPARISON, and this file is the one place
// that makes both halves of it (Rule 2). It is pure: the numbers go in, the
// sentences come out, and TestChatReplaySummaryNeverClaimsCompletionWhenShort
// drives it with the measured shape.

import "fmt"

// chatReplayTurnGap is one turn's consumption measured against the recording:
// how many of its recorded responses the surface asked for before the turn
// ended.
type chatReplayTurnGap struct {
	// Turn is the 1-based ordinal the driver prints (`turn 7/11`).
	Turn int
	// Recorded is how many responses the recording filed under this turn.
	Recorded int
	// Consumed is how many of them this run's surface actually asked for.
	Consumed int
}

// Short reports whether this turn consumed fewer responses than it was recorded
// with. A short turn is where the script's later responses stop being reachable:
// the stub serves the script by GLOBAL request order, so every unasked request
// hands the next turn a response that belongs to an earlier one.
func (g chatReplayTurnGap) Short() bool { return g.Consumed < g.Recorded }

// chatReplayAccounting is the whole comparison: the recording's totals, this
// run's totals, and the per-turn gaps between them.
type chatReplayAccounting struct {
	// RecordedRequests is the recording's script length (replaydrive's
	// ResponseCount — one response per model request).
	RecordedRequests int
	// ServedRequests is how many model requests this run's stub answered.
	ServedRequests int
	// RecordedResults / ServedResults are the tool-result halves of the same
	// comparison.
	RecordedResults int
	ServedResults   int
	// UnrecordedResults counts served results that had NO recorded counterpart
	// (the seam answered with an explicit refusal sentence instead).
	UnrecordedResults int
	// Overlong counts requests answered by REPEATING the last recorded response:
	// the surface asked for MORE script than the recording holds. It is the
	// mirror of the shortfall and is reported for the same reason.
	Overlong int
	Turns    int
	Typed    int
	// Gaps holds one entry per turn that ended before consuming its recorded
	// responses, in turn order.
	Gaps []chatReplayTurnGap
	// Aborted is the driver's own stop reason, nil when it ended by typing the
	// recording's `/quit`.
	Aborted error
}

// MissingRequests reports how many recorded model requests were never served.
func (a chatReplayAccounting) MissingRequests() int {
	return shortfallOf(a.RecordedRequests, a.ServedRequests)
}

// MissingResults reports how many recorded tool results never reached the model.
func (a chatReplayAccounting) MissingResults() int {
	return shortfallOf(a.RecordedResults, a.ServedResults)
}

// Short reports whether this run failed to consume the recording it re-ran. It
// is the ONE predicate the summary line and the exit status both read, so
// "printed a shortfall" and "admitted a shortfall" cannot disagree.
func (a chatReplayAccounting) Short() bool {
	return a.MissingRequests() > 0 || a.MissingResults() > 0 || a.UnrecordedResults > 0 || a.Aborted != nil
}

// shortfallOf is a difference clamped at zero: a run that served MORE than the
// recording holds has no missing rows (it has overlong ones, counted apart).
func shortfallOf(recorded, served int) int {
	if served >= recorded {
		return 0
	}
	return recorded - served
}

// The summary's vocabulary. Every sentence this run says about its own
// completeness is spelled ONCE here (Rule 2), so a reader can compare two runs
// and a test can pin the words.
const (
	// chatReplayAccountLine is the comparison itself, always printed: both sides
	// of every count, so "82" can never be read as "all of them".
	chatReplayAccountLine = chatReplayNotice + "served: %d of %d recorded model request(s), %d of %d recorded tool result(s), turns typed: %d/%d\n"

	// chatReplayAccountComplete is what replaces the shortfall when there IS no
	// gap. It is a claim, and it is only reachable when the numbers agree.
	chatReplayAccountComplete = chatReplayNotice + "COMPLETE: every recorded response and every recorded tool result was consumed — this run reproduced the recording's script in full\n"

	// chatReplayShortfallHead names the gap, the two totals and the mechanism.
	// "Did not reproduce the recording" is the sentence the pre-fix line lacked.
	chatReplayShortfallHead = chatReplayNotice + "SHORTFALL: this run did NOT reproduce the recording — %d of %d recorded model request(s) were never served and %d of %d recorded tool result(s) never reached the model. The stub serves the script in GLOBAL request order, so a turn that ends before consuming its own responses hands every later turn an earlier turn's answer: turns typed %d/%d says the keyboard did its job, not that the script was consumed. Two causes are possible and this run cannot tell them apart from the counts alone: the surface ended a turn early, or the recording's responses belong to HARNESS-INJECTED rounds (a `[system] Background job … finished` notice) which a replay never re-emits — it answers the job tool from the recording, so no background process runs, so nothing ever finishes.\n"

	// chatReplayShortfallTurn is one gap row. It names HOW the turn ended
	// (the witness), because "the surface went quiet" and "the driver gave up"
	// are different facts about the same missing responses.
	chatReplayShortfallTurn = chatReplayNotice + "  turn %d of %d: consumed %d of %d recorded response(s) (%d short) — the turn ended %s\n"

	// chatReplayEndedBySurface is HOW a short turn ended, in the summary's own
	// words. It is a named constant because the sentence is a CLAIM about the
	// mechanism: the surface stopped asking (the driver's quiescence witness),
	// not that the driver gave up.
	chatReplayEndedBySurface = "because the surface stopped asking for it (quiescence witness)"

	// chatReplayOverlongLine reports requests beyond the script (the mirror gap).
	chatReplayOverlongLine = chatReplayNotice + "  %d request(s) beyond the recording's script were answered by repeating the last recorded response\n"

	// chatReplayAbortedLine reports a driver that stopped for its own reason.
	chatReplayAbortedLine = chatReplayNotice + "  the driver stopped with its own error: %v\n"
)

// chatReplaySummaryLines is the run's account of its own completeness, as lines
// ready to print. It is a function rather than a block of prints so the claim can
// be tested without a terminal, a pty or a recording (Rule 31's spirit: the
// surface a run CHANGES must be executable; a claim about numbers is executable
// in a unit test, unlike a keystroke).
func chatReplaySummaryLines(a chatReplayAccounting) []string {
	lines := []string{fmt.Sprintf(chatReplayAccountLine,
		a.ServedRequests, a.RecordedRequests, a.ServedResults, a.RecordedResults, a.Typed, a.Turns)}
	if !a.Short() {
		return append(lines, chatReplayAccountComplete)
	}
	lines = append(lines, fmt.Sprintf(chatReplayShortfallHead,
		a.MissingRequests(), a.RecordedRequests, a.MissingResults(), a.RecordedResults, a.Typed, a.Turns))
	for _, g := range a.Gaps {
		if !g.Short() {
			continue
		}
		lines = append(lines, fmt.Sprintf(chatReplayShortfallTurn,
			g.Turn, a.Turns, g.Consumed, g.Recorded, g.Recorded-g.Consumed, chatReplayEndedBySurface))
	}
	if a.Overlong > 0 {
		lines = append(lines, fmt.Sprintf(chatReplayOverlongLine, a.Overlong))
	}
	if a.Aborted != nil {
		lines = append(lines, fmt.Sprintf(chatReplayAbortedLine, a.Aborted))
	}
	return lines
}

// closeTurn records what ONE turn consumed against what the recording filed
// under it, and says so AT THE MOMENT IT HAPPENS.
//
// WHY THE LIVE NOTICE STAYS even though the closeout now summarises everything
// (Rule 15): a run that advances past a short turn is making a decision the
// operator watching it should see at the turn boundary, not three minutes later
// in a footer. The footer exists so the RUN's completion claim is honest; this
// line exists so a watcher can interrupt a replay that has begun to drift.
//
// timedOut distinguishes the two ways a turn ends short — the surface went quiet
// with responses still on the table (the quiescence arm, which the pre-fix code
// read as success) versus the bound expiring while the count was still moving.
func (r *chatReplay) closeTurn(turn, recorded, consumed int, timedOut bool) {
	if consumed < 0 {
		consumed = 0
	}
	if timedOut {
		r.shortfall++
		chatNoticef(chatReplayTurnTimeoutNotice, turn, r.turns, chatReplayTurnTimeout, consumed, recorded)
	} else if consumed < recorded {
		r.shortfall++
		chatNoticef(chatReplayShortfallNotice, turn, r.turns, consumed, recorded, chatReplayQuiescence)
	}
	if consumed < recorded {
		r.gaps = append(r.gaps, chatReplayTurnGap{Turn: turn, Recorded: recorded, Consumed: consumed})
	}
}

// accounting assembles the closeout's comparison from the driver and the stub.
// It is the ONE place the two sides of every count are read together (Rule 2),
// so the summary cannot compare one run's numbers with another recording's.
func (r *chatReplay) accounting() chatReplayAccounting {
	a := chatReplayAccounting{
		RecordedRequests: r.rec.ResponseCount(),
		RecordedResults:  len(r.rec.Results),
		Turns:            r.turns,
		Typed:            r.typed,
		Gaps:             append([]chatReplayTurnGap(nil), r.gaps...),
		Aborted:          r.err,
	}
	if r.stub != nil {
		a.ServedRequests = r.stub.RequestCount()
		a.Overlong = r.stub.OverlongRequests()
		served := r.stub.ServedResults()
		a.ServedResults = len(served)
		for _, s := range served {
			if !s.Recorded {
				a.UnrecordedResults++
			}
		}
	}
	return a
}
