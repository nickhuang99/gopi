package main

// chat_surface_bound_e2e_test.go — THE BOUND, end to end, on the real REPL: the
// self-driven loop must still STOP (the absolute ceiling is retained and observed on
// the console), and it must be RENEWABLE by exactly one thing — a line the OPERATOR
// submitted (the ruling on task 01a0e44a: reset-per-user-input, no model
// self-renewal).
//
// WHY THIS CASE IS AT THE FULL STACK RATHER THAN AT THE AGENT. The counter and its
// reset live in internal/agent, and its own gate
// (internal/agent/chat_surface_renewal_test.go) drives them directly. What that gate
// CANNOT show is the half this epic made a deliverable: that the reset is wired to
// the carrier's READ arm — the one path a model-written line can never take — and
// that the operator SEES both events (the closure and the renewal) on the console
// they are watching. So the assertions here are on the PAINTED GRID of a live
// `gopi chat`, driven by a mock model, and the operator's own line is written on the
// pty master — the only line in this case that is not the model's.
//
// THE MODEL CANNOT RENEW ITSELF, AND THIS CASE MEASURES IT: the mock asks for a
// self-driven turn on EVERY round until the tool refuses, so the refusal is reached
// by the model's own persistence. Only then does the operator's line arrive, and the
// next self-driven turn is accepted against a counter that reads 1 of 8 again.

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/lineedit"
	"github.com/nick/gopi/internal/testutil"
	"github.com/nick/gopi/internal/toolnames"
)

const (
	// boundModel / boundCols / boundRows describe the live session this case runs.
	boundModel = "deepseek-v4-pro"
	boundCols  = 100
	boundRows  = 30
	// boundBootstrap opens the session: the model runs no round before a line
	// arrives, so something must be typed on the master before anything can drive.
	boundBootstrap = "start"
	// boundPeriodDone is the mock's text once the loop has been REFUSED — the marker
	// the test waits on before it knows the ceiling was reached.
	boundPeriodDone = "PERIOD-ONE-DONE"
	// boundSecondDone is the same marker one period later, after the operator's line
	// renewed the budget and the loop ran out a SECOND time.
	boundSecondDone = "PERIOD-TWO-DONE"
	// boundRenewedLine is the line the OPERATOR submits between the two periods. It is
	// written on the pty master, never injected, because it is the whole point: only a
	// line that arrived through the carrier's read arm renews the budget.
	boundRenewedLine = "carry on"
	// announcementQuoteWidth bounds how much of one announcement a failure dump quotes,
	// so a red run's ledger stays readable.
	announcementQuoteWidth = 64
	// boundPeriodFirstTurn is the self-driven turn number EVERY period numbers first:
	// the ceiling is a PER-PERIOD budget, and the renewed period numbers its own first
	// turn 1 of the bound AGAIN. That renumbering is the whole reason a whole-run count
	// of the numbered notice cannot answer a per-period question (measured: the pre-fix
	// count read 2 for a console behaving exactly as designed).
	boundPeriodFirstTurn = 1
	// boundFirstPeriod / boundRenewedPeriod are the two periods this case runs, in order.
	boundFirstPeriod   = 1
	boundRenewedPeriod = 2
	// boundStep paces every wait in this case.
	boundStep = 5 * time.Millisecond
	// boundWait is the wall-clock ceiling of a wait whose subject is a PAINT: the
	// notices, the periods and the announcements are read off the console, and
	// the console is the slowest writer in the fixture (every self-driven round
	// repaints the band, the line and the status row). It is a CEILING, not a
	// criterion — a wait that reaches it reports a console that never carried
	// the record it was waiting for, which is the assertion. The closeout is NOT
	// measured by this constant: whether the run STOPPED is a question about its
	// own progress, and it is decided by boundStallWindow (see
	// exitAfterInterrupt).
	boundWait = 20 * time.Second
	// boundStreamTailRunes bounds how much of the durable stream a counted-claim
	// failure quotes: enough to see the record, not a whole session's bytes.
	boundStreamTailRunes = 1200
	// boundStallWindow is how long the closeout tolerates a run that makes NO
	// progress at all — no model round served AND not one byte painted. It is
	// the criterion the closeout decides on: "the run stopped making progress",
	// measured against the run's own two counters. The failing shape this
	// replaces is named in the run's own log — `45 model round(s) served so far`
	// at 2.0s and the SAME 45 at 18.0s, with the goroutine still running — i.e.
	// a parked run reported as a merely slow one, decided by a wall clock that
	// says nothing about whether the run is moving. Under contention the same
	// 20s is spent by work that IS progressing (measured 2026-10-05: a whole
	// `./cmd/agent` run at load 15.18/4 cores takes 17-19 minutes against 910s
	// quiet), so the window is measured in PROGRESS, never in elapsed time.
	boundStallWindow = 5 * time.Second
	// boundStallNoteInterval paces the closeout's progress line: one line per
	// interval while the run is still moving, so the log shows WHAT the run kept
	// doing up to the moment it either left or stopped. A constant, never a poll
	// count, so the pacing cannot drift with boundStallWindow.
	boundStallNoteInterval = 1 * time.Second
	// boundCloseoutCeiling is the BACKSTOP, not the criterion: a run that is
	// STILL making progress when it expires is not a stalled closeout but a loop
	// that does not terminate, and the two failures are reported as different
	// sentences (see exitAfterInterrupt). It is generous on purpose — under
	// contention a legitimately slow turn may take a long time, and only the
	// absence of progress is evidence against it.
	boundCloseoutCeiling = 3 * time.Minute
	// boundProbeWindow / boundProbeStep pace the ctrl-c closeout PROBE (the
	// measurement this task exists for, removed once the mechanism is fixed).
	boundProbeWindow = 3 * time.Second
	boundProbeStep   = 25 * time.Millisecond
	// boundProbeStallWindow is the stall window the RED PROOF drives the FACTORED
	// closeout wait at its own scale (TestBoundCloseoutWaitFiresWhenTheRunStopsProgressing,
	// chat_surface_bound_closeout_wait_test.go), the same way chatTurnStatus.liveTick
	// lets a test drive a cadence instead of waiting out the production period: the
	// criterion is what the proof is about (boundStallWindow's progress rule), and
	// proving it does not need a 5-second wait. It is a PROOF's scale, never a second
	// policy — the release path passes boundStallWindow.
	boundProbeStallWindow = 300 * time.Millisecond
	// boundProbeProgressStep paces the red proof's "the run is still moving" leg.
	boundProbeProgressStep = 10 * time.Millisecond
	// boundRefusalMarker is the wording the TOOL uses when it refuses a `type` at the
	// ceiling (internal/agent/chat_surface.go: "this period's self-driven loop is at
	// its bound (N of M turns used)"). The mock classifies its next answer by it, so it
	// is a bare string keyword with meaning and lives here once (Rule 1/13).
	boundRefusalMarker = "at its bound"
	// boundToolResultTag / boundTextTag are the two content-block types a wire message
	// can carry for this fixture's purposes: a block answering a tool call, and a block
	// of text. They are compared, never searched for as prose, so they are constants.
	boundToolResultTag = `"type":"tool_result"`
	boundTextTag       = `"type":"text"`
)

// boundRoundDecision reports what the round that just arrived is ANSWERING, from the
// LAST content block of the LAST user message — the one block the model was actually
// handed this round.
//
// THE DEFECT THIS REPLACES, MEASURED (task 01a1bd8f, e2e red at
// chat_surface_bound_e2e_test.go:1449 with the console showing "budget RENEWED" and
// the next self-driven turn numbered 21 of a 20-turn bound). The classifier used to be
// `strings.Contains(tail, "at its bound")` over the whole tail of the last user
// message — and a chat turn's history is FOLDED (internal/agent turnfold): the round
// after a turn completes opens with a one-line recap carrying that turn's `anchors:`,
// which quote the tool result verbatim. So from the first refusal onwards EVERY round's
// tail contained the phrase "at its bound" inside an ANCHOR, and the mock classified
// every round after it as a fresh refusal. The measured trace:
//
//	round 42  answeredAlready=true  refused=true   ← the real refusal (tool_result block)
//	round 43  answeredAlready=false refused=true   ← the OPERATOR's renewed line "carry on"
//	(the run ends here: the mock answers boundPeriodDone forever, so period 2 never asks)
//
// ⇒ the operator's own line was swallowed as if it were a refusal, the renewal was
// announced on the console (the production code was RIGHT), and the renewed period never
// started because the FIXTURE refused to drive it. The failure read as "the renewed
// period announced no self-driven turn 1/20", i.e. as a defect in the code under test.
//
// THE RULE: read the CONTENT BLOCK KIND, never a substring of the message. A round
// answering a tool call is a tool_result block; a round carrying the operator's line is a
// text block and is an ASK (drive it). A refusal is only ever the tool_result block that
// carries the marker — an anchor inside a text block is a quote ABOUT the past, not the
// present answer.
func boundRoundDecision(body string) (answeredAlready, refused bool) {
	tail := body
	if i := strings.LastIndex(tail, `"role":"user"`); i >= 0 {
		tail = tail[i:]
	}
	tr := strings.LastIndex(tail, boundToolResultTag)
	if tr < 0 || tr < strings.LastIndex(tail, boundTextTag) {
		// The last block of the message is TEXT (or there is no tool result at all):
		// the model is being asked to drive, so this is an ASK. An anchor quoting an
		// old refusal inside that text must not change the verdict.
		return false, false
	}
	// The last block IS a tool result: this round carries the tool's decision, and it
	// is a refusal only when THAT block carries the marker.
	return true, strings.Contains(tail[tr:], boundRefusalMarker)
}

// boundSelfDrivenLine is the line the model asks the tool to type, numbered so the
// transcript shows which self-driven turn is which.
func boundSelfDrivenLine(n int) string {
	return fmt.Sprintf("self-driven-%02d", n)
}

// boundPeriodFirstIndex reports the turn index the wire record gives the nth period's
// FIRST accepted turn. The mock numbers its asks CONTINUOUSLY across the run, and each
// period spends bound accepted asks PLUS the one ask the ceiling refuses (the 9th ask of
// an 8-turn period — the measured shape: 18 asks over two periods of 9), so the renewed
// period's first accepted turn is bound+1 indices after the first period's. That makes
// this a POSITION in the record (which index to slice from), never a claim about the loop.
func boundPeriodFirstIndex(period, bound int) int {
	return boundPeriodFirstTurn + (period-1)*(bound+1)
}

// boundMockSSE streams one round that either asks for a self-driven turn or answers
// with text.
func boundMockSSE(w io.Writer, line string) {
	testutil.WriteClaudeSSE(w, "message_start", `{"type":"message_start","message":{"id":"m1","usage":{"input_tokens":10,"output_tokens":1}}}`)
	testutil.WriteClaudeSSE(w, "content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
	testutil.WriteClaudeSSE(w, "content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"driving"}}`)
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":0}`)
	cmd, _ := jsonRaw(map[string]string{"action": agent.ChatSurfaceActionType, "text": line}) //stylegate:ignore jsonRaw marshals a frame this test itself composed; the surface's answer is the contract
	block := fmt.Sprintf(`{"type":"tool_use","id":"call_1","name":%q,"input":%s}`, toolnames.ToolChatSurface, cmd)
	testutil.WriteClaudeSSE(w, "content_block_start", fmt.Sprintf(`{"type":"content_block_start","index":1,"content_block":%s}`, block))
	testutil.WriteClaudeSSE(w, "content_block_stop", `{"type":"content_block_stop","index":1}`)
	testutil.WriteClaudeSSE(w, "message_delta", `{"type":"message_delta","delta":{"stop_reason":"tool_use","stop_sequence":null},"usage":{"input_tokens":10,"output_tokens":7}}`)
	testutil.WriteClaudeSSE(w, "message_stop", `{"type":"message_stop"}`)
}

// jsonRaw marshals a small map to JSON (a local helper keeps the encoding import
// out of the file's public surface).
func jsonRaw(m map[string]string) ([]byte, error) {
	b := make([]byte, 0, 128)
	b = append(b, '{')
	first := true
	for k, v := range m {
		if !first {
			b = append(b, ',')
		}
		first = false
		b = append(b, '"')
		b = append(b, k...)
		b = append(b, '"', ':', '"')
		b = append(b, v...)
		b = append(b, '"')
	}
	return append(b, '}'), nil
}

// streamANSIFree returns everything painted so far with ANSI escape sequences removed,
// flattened so a notice that the 100-column pty WRAPPED onto a second row matches the
// constant.
//
// IT IS NOT A LOG OF ANNOUNCEMENTS AND MUST NOT BE COUNTED AS ONE — it is the record of
// what was WRITTEN, escape sequences and partial-row repaints included (see
// sawNoticeOnce for the two writers behind one notice, and for what the exact-count
// claim reads instead). It is the right instrument for "did this wording reach the
// operator's console", which is what the phase waits below ask.
func (g *helpMenuGrid) streamANSIFree() string {
	g.mu.Lock()
	raw := append([]byte(nil), g.data...)
	g.mu.Unlock()
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); {
		if raw[i] == 0x1b && i+1 < len(raw) && raw[i+1] == '[' {
			i += 2
			for i < len(raw) && !(raw[i] >= 0x40 && raw[i] <= 0x7e) {
				i++
			}
			i++ // the final byte
			continue
		}
		out = append(out, raw[i])
		i++
	}
	return flattened(string(out))
}

// sawNotice waits until a console notice has been SEEN, reading the durable stream (see
// streamANSIFree) rather than the current screen.
func (s *helpMenuSession) sawNotice(t *testing.T, what, format string, args ...interface{}) {
	t.Helper()
	want := flattened(fmt.Sprintf(format, args...))
	deadline := time.Now().Add(boundWait)
	for time.Now().Before(deadline) {
		if strings.Contains(s.paint.streamANSIFree(), want) {
			return
		}
		time.Sleep(boundStep) //stylegate:allow-sleep bounded poll (boundWait) for a console notice to reach the painted stream
	}
	t.Fatalf("timed out waiting for %s\n--- grid ---\n%s", what, s.rows())
}

// exitAfterInterrupt waits for the REPL to LEAVE after the ctrl-c was injected,
// and reports the run's OWN state when it does not.
//
// ITS CRITERION IS PROGRESS, NOT A CLOCK (reworked 2026-10-05, task 01a10a45;
// the wall-clock form it replaces is preserved in the devdoc). The old wait
// expired at boundWait=20s whatever the run was doing, which merged two
// different defects into one sentence and one test result. Measured on the whole
// `./cmd/agent` package at load 15.18/4 cores (2026-10-05, stream
// run-1791172666-79126.json): the progress line printed `45 model round(s)
// served so far` at 2.0s and the IDENTICAL 45 at 18.0s, then the case failed at
// 22.06s. Both counters frozen for 16s and the goroutine still running = the run
// was PARKED, not slow — while the same package legitimately needs 910s quiet
// and 1053–1154s under contention, so any wall-clock value is either too short
// for a working run or too long for a dead one. The wait now decides on the
// run's own two counters (model rounds served, bytes painted) and needs
// boundStallWindow of NO movement to fail, with boundCloseoutCeiling as a
// separate sentence for a run that never stops moving at all.
//
// WHAT IS A PROGRESS FACT, AND WHY BOTH HALVES ARE NEEDED: the mock's served
// model-round count (the fixture's own statement about the wire) AND the painted
// byte count (the editor's own statement about the console). On the measured
// failing run the round count stood still for the whole window while the paint
// kept moving, so a round-only predicate would have declared a working run stuck;
// a paint-only predicate would call a wedged editor live. Either one moving is
// the session doing something, which is what "making progress" means — and neither
// is loosened: the case's claim (one operator ctrl-c terminates the session) is
// asserted exactly as before, and it still fails, on the same message, when the
// run stops moving for boundStallWindow.
//
// THE WAIT IS NOW A FUNCTION, AND THAT IS WHAT MAKES IT PROVABLE (task
// 01a10a47, 2026-10-05): the criterion above used to live inside this method,
// where nothing could drive it — a predicate a test cannot drive is a predicate
// nobody has proven. It is now waitForCloseoutProgress (below), a function of the
// run's state — the exit channel, the progress closure, the stall window, the
// ceiling, the poll step — returning WHICH of the three exits ended it and the
// longest no-progress gap it saw. THIS method owns the meaning of the outcome
// (its three sentences) and the pacing of the log; the wait itself never logs and
// never fails anything (Rule 15). The longest gap is reported on EVERY run, so a
// reader can see what a working session's silence actually measures on this host
// — that measurement is how the stall window is derived from behaviour instead of
// guessed. The RED PROOF is its own file
// (chat_surface_bound_closeout_wait_test.go): a run that STOPS MOVING must end
// closeoutStalled whatever the clock says, and one that keeps moving must not.
//
// WHAT WAS MEASURED, AND WHAT WAS RULED OUT (so a later reader does not re-walk
// it): the editor's per-turn seam DOES make ctrl-c mean "abandon THIS turn"
// whenever MinTurnActive() is true (editor.go SetMinTurnActive / InterruptTurn;
// the REPL installs turnRunning(sess)), and that mechanism WOULD explain this
// failure exactly. It is NOT this failure. A probe that injected the key the
// instant the second period's closure marker became visible and logged the
// predicate read MinTurnActive=false at that instant — the key was at the prompt,
// so the per-turn claim is not what kept the session open (measured twice: the
// probe made the run fail EARLIER, at the period-2 marker, because the key
// genuinely ended the session). Waiting on that predicate would have been an
// unjustified change wearing a fix's clothes, so it was removed rather than
// shipped. What remains to be measured is where the key goes in the window
// between one turn returning and the read loop parking on the next read; the
// instrument this wait installs is the means to measure it.
//
// NO GRID IN THE MESSAGE, deliberately: this is the closeout, the reserved layout
// is being taken down on another goroutine, and a read of the model from HERE is a
// read of a model being torn down (a torn read is not evidence). What is reported
// instead is the run's OWN state.
//
// A SECOND CTRL-C IS NOT THE FIX: it would be a second key on the same seam and
// would change what this case asserts (one operator key terminates the session).
func (s *helpMenuSession) exitAfterInterrupt(t *testing.T, rounds func() int) {
	t.Helper()
	start := time.Now()
	// progress is the pair a stall is read from — see this function's header. It is
	// read ONCE per poll by the wait below, so both halves describe the same instant.
	progress := func() (served, painted int) { return rounds(), s.paint.painted() }
	// THE WAIT ITSELF LIVES IN waitForCloseoutProgress, so the criterion can be driven
	// by a test (chat_surface_bound_closeout_wait_test.go) instead of being asserted by
	// reading this method. What stays HERE is the meaning of each outcome and the
	// pacing of the log — the wait never logs and never fails anything (Rule 15).
	outcome, longestGap := waitForCloseoutProgress(s.exited, progress, boundStallWindow, boundCloseoutCeiling, boundStep,
		func(elapsed, since time.Duration, served, painted int) {
			// The pacing is a constant interval, and the line carries BOTH counters plus
			// how long they have been still — so the log says what the run kept doing,
			// and the reader can see the stall forming instead of meeting it only at the
			// failure.
			t.Logf("ctrl-c closeout: the run has not left yet (%v elapsed; the key claims the turn in flight, so the loop finishes that turn first; no progress for %v — %d model round(s) served, %d byte(s) painted)",
				elapsed.Round(time.Millisecond), since.Round(time.Millisecond), served, painted)
		})
	elapsed := time.Since(start).Round(time.Millisecond)
	switch outcome {
	case closeoutLeft:
		t.Logf("the REPL left %v after the injected ctrl-c (%d model round(s) served, %d byte(s) painted; the longest no-progress gap this closeout saw was %v)",
			elapsed, rounds(), s.paint.painted(), longestGap.Round(time.Millisecond))
	case closeoutStalled:
		t.Fatalf("the run STOPPED MAKING PROGRESS %v after the injected ctrl-c: neither counter moved for %v (bound boundStallWindow=%v) — %d model round(s) served and %d byte(s) painted, and the goroutine is %s. A key READ while a turn was in flight claims that turn (the editor's at-a-turn meaning), so the loop closes it out before the next read, where the same key means leave; a run that is neither moving nor leaving has stopped, whatever the clock says\n--- durable tail (does it carry the interrupt marker? a key that was READ and CLAIMED wrote one; a key that was never read wrote nothing) ---\n%s",
			elapsed, longestGap.Round(time.Millisecond), boundStallWindow,
			rounds(), s.paint.painted(), runState(s.exited), s.paint.streamTail())
	case closeoutCeiling:
		t.Fatalf("the run was STILL MAKING PROGRESS %v after the injected ctrl-c and hit the closeout ceiling boundCloseoutCeiling=%v: %d model round(s) served and %d byte(s) painted, last movement %v ago, and the goroutine is %s. This is NOT a stalled closeout — the counters are moving, so the loop is running: what failed is that it does not terminate at all, and the state above says which",
			elapsed, boundCloseoutCeiling,
			rounds(), s.paint.painted(), longestGap.Round(time.Millisecond), runState(s.exited))
	}
}

// closeoutWaitOutcome says WHY the closeout wait ended — the three exits are the
// three things that can happen to a session after the operator's key, and they mean
// different things (see exitAfterInterrupt, which owns their sentences).
type closeoutWaitOutcome int

const (
	// closeoutLeft: the session ENDED. The only outcome that satisfies the case's
	// claim (one operator ctrl-c terminates the session).
	closeoutLeft closeoutWaitOutcome = iota
	// closeoutStalled: the session produced nothing for a whole stall window. This is
	// the defect the case exists to catch.
	closeoutStalled
	// closeoutCeiling: the session was STILL MOVING when the hang ceiling was reached
	// — a livelock, not a slow host, and deliberately not the claim.
	closeoutCeiling
)

// waitForCloseoutProgress IS the closeout wait, factored out of exitAfterInterrupt so
// the criterion can be driven directly at its own scale
// (TestBoundCloseoutWaitFiresWhenTheRunStopsProgressing, in
// chat_surface_bound_closeout_wait_test.go) — a predicate a test cannot drive is a
// predicate nobody has proven.
//
// THE CRITERION IS PROGRESS, NEVER A CLOCK: the wait returns closeoutLeft the moment
// the run leaves, and otherwise ends when the run produces NOTHING for `stall`, or —
// only as a HANG DETECTOR — when a run that KEEPS moving reaches `ceiling`. `progress`
// reports (model rounds served, bytes painted), the two halves of "the session is
// doing something" (exitAfterInterrupt's header states why BOTH are needed: either one
// moving is progress). It returns the longest interval the run ever went without a
// change, which callers report: that measurement is how the stall window is derived
// from observed behaviour instead of guessed. The CALLER decides what each outcome
// means — this function never logs and never fails anything itself (Rule 15), which is
// also what makes it drivable from a test body.
func waitForCloseoutProgress(exited <-chan struct{}, progress func() (served, painted int), stall, ceiling, step time.Duration, note func(elapsed, since time.Duration, served, painted int)) (closeoutWaitOutcome, time.Duration) {
	start := time.Now()
	lastServed, lastPainted := progress()
	lastChange := start
	longestGap := time.Duration(0)
	recordGap := func(now time.Time) {
		if gap := now.Sub(lastChange); gap > longestGap {
			longestGap = gap
		}
	}
	// noteEvery is the spacing of the progress lines — a named INTERVAL, so the pacing
	// is a CONSTANT and can never drift with the stall window (the form it replaced
	// divided the window by a count, which silently changed the pacing whenever the
	// window was retuned).
	noteEvery := boundStallNoteInterval
	nextNote := start.Add(noteEvery)
	for {
		select {
		case <-exited:
			recordGap(time.Now())
			return closeoutLeft, longestGap
		default:
		}
		now := time.Now()
		served, painted := progress()
		switch {
		case served != lastServed || painted != lastPainted:
			recordGap(now)
			lastServed, lastPainted, lastChange = served, painted, now
		case now.Sub(lastChange) >= stall:
			recordGap(now)
			return closeoutStalled, longestGap
		case now.Sub(start) >= ceiling:
			recordGap(now)
			return closeoutCeiling, longestGap
		}
		// The note is paced by a CONSTANT interval and suppressed once the run has gone
		// quiet for the whole stall window — past that point the stall is the verdict,
		// and another line would only repeat it.
		if note != nil && !now.Before(nextNote) && now.Sub(lastChange) < stall {
			note(now.Sub(start), now.Sub(lastChange), served, painted)
			nextNote = now.Add(noteEvery)
		}
		time.Sleep(step) //stylegate:allow-sleep bounded poll on the run's own progress (the caller's stall window and ceiling) for the REPL to leave after the injected ctrl-c
	}
}

// runState names what the run's goroutine is doing at the moment of failure:
// closed = it left (and the wait above missed it, a different defect entirely),
// running = it is still inside runChatWithInput.
func runState(exited chan struct{}) string {
	select {
	case <-exited:
		return "closed (the run left; the wait above missed it)"
	default:
		return "still running"
	}
}

// boundCloseoutVerdict is the closeout's DECISION, in one place, so the wait and
// its guard read the same rule (Rule 14: one definition, two consumers).
//
// It takes the two facts the wait measures — how long the run's counters have
// been still, and how long the closeout has been waiting — and answers which of
// the three states the run is in. The order is the whole point: a STALL is
// decided before the ceiling, because a stalled run must never be reported as
// "still progressing" (that sentence would name the wrong defect and the wrong
// remedy).
type boundCloseoutState int

const (
	// boundCloseoutMoving: the run's counters moved inside boundStallWindow and
	// the closeout is inside its ceiling — keep waiting.
	boundCloseoutMoving boundCloseoutState = iota
	// boundCloseoutStalled: nothing moved for boundStallWindow — the run has
	// stopped, whatever the clock says. THE FAILURE the wait exists to report.
	boundCloseoutStalled
	// boundCloseoutStillMoving: the counters are moving but the closeout has
	// been waiting boundCloseoutCeiling — a DIFFERENT defect (a loop that does
	// not terminate), and it gets its own sentence.
	boundCloseoutStillMoving
)

func boundCloseoutVerdict(stillFor, waited time.Duration) boundCloseoutState {
	if stillFor >= boundStallWindow {
		return boundCloseoutStalled
	}
	if waited >= boundCloseoutCeiling {
		return boundCloseoutStillMoving
	}
	return boundCloseoutMoving
}

// TestBoundCloseoutVerdictIsProgressNotAClock guards the rework's whole claim: the
// closeout's verdict is decided by the run's own PROGRESS, and a run that keeps
// moving is never failed for the clock's sake.
//
// THE RED PROOF is the middle case, and it is the shape that failed in production
// (run-1791172666-79126.json): the counters frozen for boundStallWindow while the
// wait has barely begun must be STALLED — the pre-fix code returned "keep waiting"
// there, because elapsed time was its only input, and then spent the remaining
// 18 seconds before reporting a defect it had already observed at 2s. Reverting
// the rework (a wall-clock-only wait) makes that case read MOVING, so the guard
// is red on the old behaviour.
func TestBoundCloseoutVerdictIsProgressNotAClock(t *testing.T) {
	frozen := boundStallWindow // the counters have not moved for exactly the window
	fresh := boundStallWindow - boundStep
	for _, tc := range []struct {
		name     string
		stillFor time.Duration
		waited   time.Duration
		want     boundCloseoutState
	}{
		{"just moved, well inside the ceiling — keep waiting", 0, boundStallWindow, boundCloseoutMoving},
		{"fresh movement at the closeout ceiling — a loop that does not terminate, reported as such", fresh, boundCloseoutCeiling, boundCloseoutStillMoving},
		{"counters frozen for the stall window — STALLED, however little time has passed", frozen, boundStallWindow, boundCloseoutStalled},
		{"counters frozen for far longer than the stall window, past the ceiling too — STALLED still, the stall is decided first", frozen * 4, boundCloseoutCeiling * 2, boundCloseoutStalled},
	} {
		if got := boundCloseoutVerdict(tc.stillFor, tc.waited); got != tc.want {
			t.Errorf("boundCloseoutVerdict(stillFor=%v, waited=%v) = %v, want %v (%s)", tc.stillFor, tc.waited, got, tc.want, tc.name)
		}
	}
}

// flattened removes EVERY whitespace character, and both sides of a notice comparison go
// through it. It exists because the pty is 100 columns and the notices are longer, so the
// grid WRAPS them onto a second row — and the wrap does not always land on a space
// (measured: the renewal notice reaches the screen as "...available again" / ") — it is
// reset only this way…", so even a blank-collapsing comparison fails on a notice that is
// present and complete, because the wrap point and the constant disagree about whether a
// space belongs there). Removing whitespace from BOTH sides makes the comparison the
// statement it is meant to be — "this wording reached the console" — and carries no claim
// about where the terminal chose to break the line.
func flattened(s string) string { return strings.Join(strings.Fields(s), "") }

// streamTail is the LAST boundStreamTailRunes of the durable stream, so a failure
// message can quote the record it counted without dumping a whole session's bytes
// into the test log.
func (g *helpMenuGrid) streamTail() string {
	full := g.streamANSIFree()
	r := []rune(full)
	if len(r) <= boundStreamTailRunes {
		return full
	}
	return string(r[len(r)-boundStreamTailRunes:])
}

// ── THE ANNOUNCEMENT LEDGER ─────────────────────────────────────────────────────
//
// The claim "the console announces each numbered self-driven turn exactly once PER
// PERIOD" is a claim about EVENTS, and a run has exactly one record of them that is not
// merged with anything else: the REPL's own notice stream (chatInput.announce,
// cmd/agent/lineedit_wire.go — WriteNotice is the ONE place a notice is announced).
//
// WHAT THE PAINTED BYTE STREAM CANNOT DO, measured in this case's own failure:
//
//	chat_surface_bound_e2e_test.go:446: the console announced the self-driven turn 1/8
//	2 time(s), want exactly 1
//
// The run that printed this was CORRECT: the renewed period numbers its own first turn
// 1 of 8 again, so a whole-RUN count of that wording is 2 in any run that completes both
// periods (the failure's stream tail quoted period 1's `1/8` and the renewed period's
// `1/8` side by side). The painted stream merges two writers (the notice's own band draw
// and the editor's incremental repaint of the rows it tracks) and carries both periods'
// identical numbering, so it can answer "this wording REACHED the console" and nothing
// more. A count over it is a count of WRITES — which is why it read 2 for a console
// behaving exactly as designed.
//
// SO THE RECORD IS THE LEDGER: one entry per announcement, IN ORDER, so a period is a
// WINDOW in that order — period 1 is what ends at its own closing notice, the renewed
// period is what the renewal opened — and the identity of each period's turns is taken
// from the TEST's own act on the pty, never from a tally the loop keeps about itself.
type announcementLedger interface {
	// count reports how many announcements contain want.
	count(want string) int
	// countBetween reports how many announcements containing want sit in entries
	// [from, to) — a HALF-OPEN window, so consecutive windows compose without an entry
	// being counted twice.
	countBetween(from, to int, want string) int
	// countInWindow is countBetween with an INCLUSIVE end, for the one place a period's
	// record legitimately ends ON the boundary entry itself: the closing notice ends the
	// period it announces, so a period read up to its closing notice includes it.
	countInWindow(from, to int, want string) int
	// entriesContaining reports the POSITIONS of every announcement containing marker, in
	// the order the REPL made them — how a phase boundary is LOCATED in the record.
	entriesContaining(marker string) []int
	// countBefore reports how many announcements containing want were made up to and
	// including the FIRST entry containing marker: the window PERIOD ONE occupies, which
	// its own closing notice closes.
	countBefore(marker, want string) int
	// entries returns every announcement in the order the REPL made it, for a failure
	// message (quoted within announcementQuoteWidth so a red run's dump stays readable).
	entries() []string
	// turnNoticesAfter reports how many times the console announced the numbered
	// self-driven-turn notice for `n` of `bound` at a position STRICTLY AFTER the first
	// announcement containing `after` (the renewal notice) — the window the OPERATOR's own
	// line opened, and the only place the renewed period's `1 of the bound` can be told
	// from period 1's identical text.
	turnNoticesAfter(after string, n, bound int) int
	// decisions is the per-period WIRE record (see periodRecord): one entry per
	// `chat_surface` decision, in the order the mock served it.
	decisions() []periodTurnDecision
}

// countingWriter is the console's ANNOUNCEMENT LEDGER: the one writer every REPL notice
// goes through (chatInput.announce, cmd/agent/lineedit_wire.go), counting each notice the
// REPL ANNOUNCES.
//
// TWO RECORDS, one instrument:
//
//   - `order` is one entry per announcement (one WriteNotice call — the notice the REPL
//     wrote, flattened). The ORDER is the point: "how many times AFTER this event" is a
//     question a tally cannot answer, and it is the only way two IDENTICAL notices (the
//     first period's "1 of 8" and the renewed period's) can be told apart without
//     inventing a second observable.
//   - `periods` is the per-period WIRE record (periodRecord): written by the mock as it
//     serves each `chat_surface` decision, so a period's arithmetic is a SLICE of an
//     order rather than a subtraction of two run-scoped totals.
//
// It passes every byte through unchanged — it is an instrument, and an instrument that
// changed what it measured would be a different console.
type countingWriter struct {
	mu sync.Mutex
	// order is every announcement, flattened, one entry per WriteNotice. A row merely
	// REDRAWN on the terminal never reaches here at all, which is the distinction the
	// painted stream cannot make and a count of announcements needs.
	order []string
	// periods is the wire record the test's own mock writes into.
	periods periodRecord
	// asks is the mock's ASK SIDE of that same wire, in the same order and carrying the
	// same turn indices: one entry per self-driven request the mock handed out, marked
	// `askRefused` once the round carrying its result answered "at its bound". It is
	// what keeps the fixture's offers and the record's decisions reconcilable ONE
	// POSITION AT A TIME — the run-scoped totals are deliberately not read here (see
	// the reconciliation in the case).
	asks []wireAsk
}

// wireAsk is one self-driven ask the mock made, in the order it made it.
type wireAsk struct {
	// turnIndex is the number the mock put in the request — the same index the wire
	// record carries for that ask's outcome, which is how the two sides are paired.
	turnIndex int
	// state is askAwaiting while the ask's result round has not been served.
	state askState
}

// askState is the state of one ask: whether the round carrying its result has arrived.
type askState int

const (
	// askAwaiting is an ask still awaiting its result round, and it is the INITIAL
	// state: the mock records its ask side in the same round it hands the request out.
	askAwaiting askState = iota
	// askRefused is an ask whose result round was served as a refusal — a decision the
	// wire record also carries, which is what makes the two sides comparable.
	askRefused
)

// Write records one announcement and reports the bytes as written.
func (g *countingWriter) Write(b []byte) (int, error) {
	key := flattened(string(b))
	if key != "" {
		g.mu.Lock()
		g.order = append(g.order, key)
		g.mu.Unlock()
	}
	return len(b), nil
}

// entries returns every announcement in the order the REPL made it, for a failure
// message (quoted within announcementQuoteWidth so a red run's dump stays readable).
func (g *countingWriter) entries() []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	out := make([]string, 0, len(g.order))
	for _, text := range g.order {
		if len(text) > announcementQuoteWidth {
			text = text[:announcementQuoteWidth] + "…"
		}
		out = append(out, text)
	}
	return out
}

// count reports how many announcements contain want.
//
// CONTAINMENT, for the same reason sawNotice uses it: one entry is one WriteNotice, and
// a notice written together with its neighbours still announces each of them exactly
// once. What this must NOT do is count a second time an announcement that was merely
// redrawn — and it cannot: a redraw is a terminal write, not a Write here.
func (g *countingWriter) count(want string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, text := range g.order {
		if strings.Contains(text, want) {
			n++
		}
	}
	return n
}

// entriesContaining reports the POSITIONS of every announcement containing marker, in the
// order the REPL made them — how a phase boundary is LOCATED in the record.
func (g *countingWriter) entriesContaining(marker string) []int {
	g.mu.Lock()
	defer g.mu.Unlock()
	var at []int
	for i, text := range g.order {
		if strings.Contains(text, marker) {
			at = append(at, i)
		}
	}
	return at
}

// countBetween reports how many announcements containing want sit in entries [from, to) —
// a HALF-OPEN window, so consecutive windows compose without an entry being counted twice.
// The bounds are the entries that OPEN and CLOSE a period.
func (g *countingWriter) countBetween(from, to int, want string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	n := 0
	for _, text := range g.order[clampIndex(from, len(g.order)):clampIndex(to, len(g.order))] {
		if strings.Contains(text, want) {
			n++
		}
	}
	return n
}

// countInWindow is countBetween with an INCLUSIVE end, for the one place a period's
// record legitimately ends ON the boundary entry itself: the closing notice ends the
// period it announces, so a period read up to its closing notice includes it.
func (g *countingWriter) countInWindow(from, to int, want string) int {
	return g.countBetween(from, to+1, want)
}

// countBefore reports how many announcements containing want were made UP TO AND INCLUDING
// the FIRST entry containing marker — the window PERIOD ONE occupies, which its own closing
// notice closes. When marker never arrived it reports 0: there is then no period to count
// over, and the caller has already failed on the missing boundary (the honest answer is
// zero, never "everything so far").
func (g *countingWriter) countBefore(marker, want string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	end := -1
	for i, text := range g.order {
		if strings.Contains(text, marker) {
			end = i
			break
		}
	}
	if end < 0 {
		return 0
	}
	n := 0
	for _, text := range g.order[:end+1] {
		if strings.Contains(text, want) {
			n++
		}
	}
	return n
}

// turnNoticesAfter reports how many times the console announced the numbered
// self-driven-turn notice for `n` of `bound` AFTER the first announcement containing
// `after` (the renewal notice) — the window the OPERATOR's own line opened.
//
// It is the ordered read the phase boundary needs, and it is exact in both directions: a
// number announced before the renewal contributes zero, and the renewal's own entry is the
// boundary — the entries strictly after it are the renewed period's. That ordering is the
// ONLY way the renewed period's own `1 of the bound` can be told from period 1's identical
// text, which is the whole reason the read is scoped here and not over the run.
func (g *countingWriter) turnNoticesAfter(after string, n, bound int) int {
	want := flattened(fmt.Sprintf(chatSelfDrivenTurnNotice, n, bound))
	g.mu.Lock()
	defer g.mu.Unlock()
	marker := -1
	for i, text := range g.order {
		if strings.Contains(text, after) {
			marker = i
		}
	}
	if marker < 0 {
		// No renewal announcement yet: there is no "after" to count, and reporting 0 is the
		// honest answer (the caller fails on it, naming the missing renewal).
		return 0
	}
	count := 0
	for _, text := range g.order[marker+1:] {
		if strings.Contains(text, want) {
			count++
		}
	}
	return count
}

// clampIndex keeps a window bound inside the ledger (a caller passing an index found in an
// earlier read cannot make the read panic; the window is simply clipped to what is
// recorded now).
func clampIndex(i, n int) int {
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}

// ── THE PER-PERIOD WIRE RECORD ──────────────────────────────────────────────────
//
// "Each period accepts exactly $bound self-driven turns and is refused exactly once"
// is an arithmetic about ONE period, and every counter a test can otherwise read is
// RUN-scoped: the mock's own atomics count asks over the whole run (so a case that has
// already run both periods reads 16 accepted + 2 refused against a per-period claim —
// measured on the sibling tree: 18 asks = two periods of 9), and the API's usage block
// is the run's total too. Neither is wrong as a fact; both are the wrong WINDOW, and
// the only thing that makes a run-scoped instrument per-period is SLICING it at a
// boundary — a position in an ordered record, not a moment in time.
//
// THE ORDER IS THE TEST'S OWN, and it is why this record is not a fixture counter that
// needs trusting: every `chat_surface` request is logged AS THE MOCK SEES IT, carrying
// the turn index the request itself names (read out of the request body). A period is
// then the slice between two positions the test can point at:
//
//	period n = the accepted decisions from `first` to `first + bound - 1` (the turn
//	           indices coming out of the test's own `self-driven-NN` numbering), and the
//	           refusal decisions reported in that same slice — which are the one that
//	           CLOSES the period (its closing turn's own next ask) and the one that
//	           PRE-CHARGES the next period's first ask.
//
// Slice first, then read: no counter is compared against a number that the run has
// moved on from, and the same record answers both periods.
type periodTurnDecision struct {
	// turnIndex is the self-driven turn number this decision governs — the number the
	// request itself asked to drive (`self-driven-NN`), so it comes from the test's own
	// act on the wire and not from a tally the loop keeps about itself.
	turnIndex int
	// accepted is true when the tool took the request, false when it refused it at its
	// bound (the tool's own "at its bound" wording, which the mock already classifies
	// its answers by).
	accepted bool
}

// periodRecord is the ordered wire record: one entry per `chat_surface` DECISION.
type periodRecord struct {
	events []periodTurnDecision
}

// record logs one decision in the order the wire produced it.
func (g *countingWriter) record(ev periodTurnDecision) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.periods.events = append(g.periods.events, ev)
}

// ask logs one OFFER — the mock handing a self-driven request to the loop — as one
// entry in the ask side of the wire, left `askAwaiting` until the round that carries
// its result says otherwise.
func (g *countingWriter) ask(turnIndex int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.asks = append(g.asks, wireAsk{turnIndex: turnIndex, state: askAwaiting})
}

// serveAsk marks the ask carrying turnIndex as REFUSED — the round that carried its
// result said the loop was at its bound.
func (g *countingWriter) serveAsk(turnIndex int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for i := len(g.asks) - 1; i >= 0; i-- {
		if g.asks[i].turnIndex == turnIndex && g.asks[i].state == askAwaiting {
			g.asks[i].state = askRefused
			return
		}
	}
}

// askReconciliation pairs the two sides of the mock's OWN wire, position by position:
// every DECISION must belong to an ask the mock actually made (an `unpaired` index names
// a decision with no ask), and where an ask is decided the two sides must name the SAME
// outcome (`mismatched`).
//
// IT SAYS NOTHING ABOUT HOW FAR THE RUN HAS GOT, and that is deliberate: an ask whose
// result round has not been served yet is simply undecided, and how many are in flight
// is a statement about the run's progress — the very thing a run-scoped comparison
// borrows in and which made its predecessor red for a correct fixture (measured
// 2026-10-02, whole package: two totals compared as `asks == decisions` reported `the
// mock offered 12 self-driven request(s) but the wire record holds 10 decision(s)`).
func (g *countingWriter) askReconciliation() (mismatched, unpaired []int) {
	g.mu.Lock()
	defer g.mu.Unlock()
	asked := make(map[int]wireAsk, len(g.asks))
	for _, a := range g.asks {
		asked[a.turnIndex] = a
	}
	for _, ev := range g.periods.events {
		a, ok := asked[ev.turnIndex]
		if !ok {
			unpaired = append(unpaired, ev.turnIndex)
			continue
		}
		if (ev.accepted && a.state != askAwaiting) || (!ev.accepted && a.state != askRefused) {
			mismatched = append(mismatched, ev.turnIndex)
		}
	}
	return mismatched, unpaired
}

// decisions returns the decisions recorded so far (a copy, so a reader cannot race the
// mock's own append).
func (g *countingWriter) decisions() []periodTurnDecision {
	g.mu.Lock()
	defer g.mu.Unlock()
	return append([]periodTurnDecision(nil), g.periods.events...)
}

// periodArithmetic is ONE period's arithmetic, sliced out of the ordered wire record.
//
// TWO NUMBERINGS APPEAR HERE, and keeping them apart is the whole reason the read is
// positional (measured: a whole-run count of the ANNOUNCEMENT is 2 for a correct console,
// because the renewed period renumbers from 1):
//
//   - the WIRE record's turn indices are CONTINUOUS across the run — the mock numbers its
//     asks 1, 2, 3, … and never restarts, so the renewed period's first turn carries
//     index bound+1. Those indices are how a period is LOCATED (the slice anchor).
//   - the CONSOLE's numbering restarts at boundPeriodFirstTurn in every period ("1 of the
//     bound" again) — that renumbering is what "renewed" MEANS, and it is reported here as
//     `first`/`last`, the numbers the operator read.
type periodArithmetic struct {
	// accepted and refused are the decisions in the period's slice: exactly `bound`
	// accepted and exactly one refusal is what a spent period looks like.
	accepted, refused int
	// first and last are the CONSOLE's numbers for this period's accepted turns: 1..bound
	// for a spent period (renumbered per period, never continuing across the renewal).
	first, last int
	// wireFirst and wireLast are the continuous wire indices the same decisions carried,
	// so the slice's alignment with the test's own position in the record is checkable.
	wireFirst, wireLast int
}

// periodFrom slices the period that starts at the accepted decision whose turn index is
// `first`. The window is positional and symmetric for both periods: the accepted run
// `first … first+bound-1` plus every refusal decision at a position after that run's
// first acceptance and before the NEXT accepted decision — i.e. the asks the model made
// while the ceiling was in force, whichever side of the period boundary they sit on.
//
// The refusal that OPENS the next period (the ask refused after the renewal, before the
// renewed period's first acceptance) therefore belongs to the PREVIOUS period's
// arithmetic, and that is the honest reading, not an artefact: it is the same turn —
// the closing turn's next ask — that the pre-fix assertion counted as "the period's one
// refusal", and the period it is charged to is the one that was spent when it arrived.
// Both periods are read over the same rule, so neither is privileged, and the per-period
// claim ("bound accepted, exactly 1 refused") is asserted for EACH of them.
//
// A zero `first` (no such acceptance in the record) reports a zero-valued arithmetic:
// the caller names the missing turn in its failure.
func periodFrom(events []periodTurnDecision, first, bound int) periodArithmetic {
	var p periodArithmetic
	if first <= 0 {
		return p
	}
	// Find the position of the acceptance that STARTED this period.
	startAt := -1
	for i, ev := range events {
		if ev.accepted && ev.turnIndex == first {
			startAt = i
			break
		}
	}
	if startAt < 0 {
		return p
	}
	// Walk the slice: this period's accepted run, plus the refusals the model took while
	// its ceiling was in force. The window CLOSES at the next period's first acceptance —
	// one position past the refusal that closed this one — so the refusal is inside the
	// period that was spent when the ask arrived.
	for _, ev := range events[startAt:] {
		if ev.accepted && ev.turnIndex >= first+bound {
			break // the next period's first acceptance ends this window
		}
		if ev.accepted {
			if p.wireFirst == 0 {
				p.wireFirst = ev.turnIndex
			}
			p.accepted++
			p.wireLast = ev.turnIndex
			continue
		}
		p.refused++
	}
	if p.accepted == 0 {
		return periodArithmetic{}
	}
	// The console's own numbering: every period numbers its first turn 1 of the bound
	// (that renumbering is what "renewed" MEANS), so the numbers are derived from the
	// slice's own anchor rather than from the wire indices, which run on across the run.
	p.first = p.wireFirst - first + boundPeriodFirstTurn
	p.last = p.first + p.accepted - 1
	return p
}

// readPeriod asserts the period existed at all and returns its arithmetic, taking the
// first turn index from the TEST's own wire record (`first`), never from a counter.
func (s *helpMenuSession) readPeriod(t *testing.T, what string, first, bound int) periodArithmetic {
	t.Helper()
	// The period is COMPLETE when it is read: the loop PARKS after it closes a period
	// (it refuses every further request until the operator's line renews it — the very
	// behaviour PHASE 1 measures), so the wire record settles at the boundary. Waiting
	// for the RENEWAL here would deadlock (the renewal is the operator's line, which
	// PHASE 2 types), which is why the period's own numbering is SLICED rather than
	// waited for. A bounded wait pins the fact that the first acceptance has been
	// recorded at all.
	deadline := time.Now().Add(boundWait)
	for time.Now().Before(deadline) {
		if p := periodFrom(s.ledger.decisions(), first, bound); p.accepted > 0 {
			return p
		}
		time.Sleep(boundStep) //stylegate:allow-sleep bounded poll (boundWait) for the period's own decisions to be recorded
	}
	t.Fatalf("the wire record never carried a decision for %s (the accepted self-driven turn %d of the bound):\n%v\n--- durable stream tail ---\n%s",
		what, first, s.ledger.decisions(), s.paint.streamTail())
	return periodArithmetic{}
}

// assertPeriodArithmetic states the period's claim, unchanged in strength: the period
// accepted EXACTLY the bound, the console numbered its turns 1..bound (the renumbering a
// renewal buys), and the ceiling refused EXACTLY ONE request — all read over that
// period's own window rather than over the run.
//
// The wire indices are checked too, and that is what keeps the slice tied to the period
// the caller means: the accepted decisions must run CONSECUTIVELY from the index the
// caller anchored on.
func (s *helpMenuSession) assertPeriodArithmetic(t *testing.T, what string, period int, p periodArithmetic, bound int) {
	t.Helper()
	wantWireFirst := boundPeriodFirstIndex(period, bound)
	if p.accepted != bound || p.first != boundPeriodFirstTurn || p.last != bound {
		t.Fatalf("%s accepted %d self-driven turn(s), numbered %d..%d, want exactly %d numbered %d..%d — a period's ceiling is "+
			"%d accepted turns, and the numbering is the loop's own (each period renumbers from %d, which is what the renewal MEANS), "+
			"read over THIS period's window and never over a counter scoped to the whole run\n%v\n--- durable stream tail ---\n%s",
			what, p.accepted, p.first, p.last, bound, boundPeriodFirstTurn, bound, bound,
			boundPeriodFirstTurn, s.ledger.decisions(), s.paint.streamTail())
	}
	if p.wireFirst != wantWireFirst || p.wireLast != wantWireFirst+bound-1 {
		t.Fatalf("%s's decisions ran over wire indices %d..%d, want %d..%d — the period's window must be anchored on the turn the "+
			"TEST asked for, so a slice can never drift onto another period's turns\n%v\n--- durable stream tail ---\n%s",
			what, p.wireFirst, p.wireLast, wantWireFirst, wantWireFirst+bound-1, s.ledger.decisions(), s.paint.streamTail())
	}
	if p.refused != 1 {
		t.Fatalf("%s was refused %d time(s) at the ceiling, want exactly 1 (the model keeps OFFERING to drive, so the refusal is "+
			"reached by its own persistence) — read over this period's own window\n%v\n--- durable stream tail ---\n%s",
			what, p.refused, s.ledger.decisions(), s.paint.streamTail())
	}
}

// assertRenewedNumbering asserts the RENEWED period re-opened the numbering: the
// operator's own line buys a fresh budget, so the period it opens is numbered from
// boundPeriodFirstTurn again ("1 of the bound"), which is exactly what the pre-fix
// whole-run count could not express (it read 2 for that wording and called a correct
// console a defect).
//
// The window is the POSITION the renewal occupies in the ledger's order — the entries
// strictly AFTER it — and the first period's own "1 of the bound" sits BEFORE that
// position, so it cannot enter this count. The read is taken over a window that is
// complete when it is read only once the renewed period has produced its numbering, so
// the wait is the bounded one every other read in this case uses; it is asserted here
// and re-asserted by the period's own arithmetic in PHASE 3.
func (s *helpMenuSession) assertRenewedNumbering(t *testing.T, renewalAt, bound int) {
	t.Helper()
	g := s.ledger.(*countingWriter)
	want := flattened(fmt.Sprintf(chatSelfDrivenTurnNotice, boundPeriodFirstTurn, bound))
	deadline := time.Now().Add(boundWait)
	got := 0
	for time.Now().Before(deadline) {
		got = 0
		g.mu.Lock()
		for _, text := range g.order[clampIndex(renewalAt+1, len(g.order)):] {
			if strings.Contains(text, want) {
				got++
			}
		}
		g.mu.Unlock()
		if got > 0 {
			break
		}
		time.Sleep(boundStep) //stylegate:allow-sleep bounded poll (boundWait) for the renewed period's first numbered turn
	}
	if got == 0 {
		t.Fatalf("the renewed period announced no self-driven turn %d/%d after the renewal, want it announced — the operator's line must open a fresh "+
			"period whose first self-driven turn is accepted and numbered %d of the bound again (a spent period would have refused it at its first "+
			"attempt, which PHASE 1 pins)\n%v\n--- durable stream tail ---\n%s",
			boundPeriodFirstTurn, bound, boundPeriodFirstTurn, g.entries(), s.paint.streamTail())
	}
}

// sawNoticeOnce asserts a notice was ANNOUNCED exactly once, and returns what the two
// records say.
//
// It is used for the notices a period announces ONCE (the renewal, the two closures); a
// per-period claim about the NUMBERED turns is made over a period WINDOW instead (see
// readPeriod), because the same wording is announced once in EACH of the two periods and
// a whole-run count of it is 2 by design.
//
// The two counts are returned separately and neither is a verdict on its own: the LEDGER
// count is the claim (it counts events), and the PAINTED count is evidence — it also
// counts a repaint of the same row, so it reads 2 for a notice announced once. When the
// ledger says 1 the claim holds; when it says 0 but the wording was painted exactly once
// the record is AMBIGUOUS and is reported as ambiguous rather than resolved by a guess
// (the alternative is an intermittently red case that reports nothing, which is the
// defect this task exists to close).
func (s *helpMenuSession) sawNoticeOnce(t *testing.T, what, format string, args ...interface{}) (announced, painted int) {
	t.Helper()
	want := flattened(fmt.Sprintf(format, args...))
	deadline := time.Now().Add(boundWait)
	for time.Now().Before(deadline) {
		announced, painted = s.ledger.count(want), strings.Count(s.paint.streamANSIFree(), want)
		if announced == 1 || painted > 1 {
			break
		}
		time.Sleep(boundStep) //stylegate:allow-sleep bounded poll (boundWait) for the announcement to be counted
	}
	if announced == 1 {
		return announced, painted
	}
	// The ambiguity branch is reachable ONLY from a ledger reading of ZERO. A ledger count
	// of 2 or more is a verdict — the announcement really happened twice — and the painted
	// count may NEVER veto it (the paint stream also counts a repaint of one row, so it is
	// evidence, not an instrument); that shape would be the paint count deciding a verdict
	// in the passing direction, which is the defect this file exists to remove.
	if announced == 0 && painted == 1 {
		t.Logf("INCONCLUSIVE (not a failure): %s was painted exactly once in the durable stream but the announcement ledger counted %d "+
			"announcement(s) — the count was read before the writer reached it, or the wording reached the screen by a path this case does not know about", what, announced)
		return announced, painted
	}
	t.Fatalf("%s was announced %d time(s) by the REPL and painted %d time(s) in the durable stream, want exactly 1 announcement "+
		"— a notice genuinely announced twice is a defect (the loop renews once per operator line), and the two counts are quoted separately "+
		"because the painted one also counts a REPAINT of the same row or a second period's own write\n--- durable stream tail ---\n%s",
		what, announced, painted, s.paint.streamTail())
	return announced, painted
}

// notice waits for a console notice, comparing the flattened LIVE grid against the
// flattened notice — "the operator can read it right now". The notice is passed as the
// format plus its arguments, so the constants remain the single source of the wording
// (Rule 1b).
func (s *helpMenuSession) notice(t *testing.T, what, format string, args ...interface{}) {
	t.Helper()
	want := flattened(fmt.Sprintf(format, args...))
	s.waitFor(t, what, func(grid string) bool { return strings.Contains(flattened(grid), want) })
}

// TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator is the bound's
// end-to-end acceptance.
func TestChatSurfaceBound_TerminatesAndIsRenewedOnlyByTheOperator(t *testing.T) {
	t.Setenv("GOPI_HOME", t.TempDir())
	t.Setenv("GOPI_MEMORY_DB", "")

	s := &helpMenuSession{exited: make(chan struct{})}
	// THE ANNOUNCEMENT LEDGER, and the wire record it carries. The ledger is where
	// every notice the REPL announces lands, and its ORDER is what makes a per-period
	// claim expressible at all (the two periods announce the SAME wording); the wire
	// record it also carries is where each `chat_surface` decision is logged as the
	// mock serves it, so a period's arithmetic is a SLICE of an order rather than a
	// subtraction of two run-scoped totals (see periodRecord).
	ledger := &countingWriter{}
	s.ledger = ledger
	// pendingAskTurn is the turn number of the last `type` request this mock SERVED, held
	// until the round that carries its outcome (the tool's result). An ask and its
	// outcome are two rounds apart — the model asks, the tool decides, the result comes
	// back — so a record written at the ASK would state a decision the tool had not made
	// yet, and the period boundary would land one round early. That is exactly the
	// off-by-one this field removes: measured with the record written at the ask, the
	// refusal that closes a period sat AFTER the next period's first ask and the
	// period's own read reported `refused 0, want exactly 1`.
	var pendingAskTurn int32
	var turns int32
	var refusals int32
	var boundShared int32
	var mu sync.Mutex
	var bodies []string
	// roundsServed reports how many model rounds the mock has answered. The closeout
	// wait's own progress line reads it, so "the run is still working through the turn
	// the key claimed" is distinguishable from "the run has stopped" (measured: the
	// failing report said only "did not finish", with no state at all).
	roundsServed := func() int {
		mu.Lock()
		defer mu.Unlock()
		return len(bodies)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)         //stylegate:ignore bounded read of a test request body; a short read is acceptable
		defer func() { _ = r.Body.Close() }() //stylegate:ignore the test's own request body close
		mu.Lock()
		bodies = append(bodies, string(body))
		mu.Unlock()
		w.Header().Set("Content-Type", "text/event-stream")

		// THE SHAPE OF ONE SELF-DRIVEN TURN, and why the mock must END its turn: the
		// bound is charged at the TURN BOUNDARY (ChatSurfaceTakePending), so a mock that
		// kept calling `type` inside one turn would replace its own pending line forever
		// and never reach the seam — measured: the turn died at the agent's own
		// 40-tool-round ceiling with the counter still at zero. So the mock types a line
		// and then ANSWERS, which lets the typed line become the next turn: round 1 of a
		// turn asks, the round carrying the tool result answers.
		//
		// The message the model is answering is the LAST one on the wire. Everything this
		// fixture decides is decided from THAT message only, never from the whole body:
		// the conversation replays, so an earlier tool result stays in the history and a
		// whole-body search would let a stale refusal drive every later round (measured —
		// the first version searched the full body for "at its bound" and so answered the
		// period's closure marker to every round after the first refusal, which read as
		// "period 2 never started" while the console was quite properly running it).
		answeredAlready, refused := boundRoundDecision(string(body))

		// WHAT THE MOCK IS ANSWERING IS DECIDED ABOVE; THE DECISION IS LOGGED HERE,
		// in the order the wire produced it. One entry per `chat_surface` decision, so
		// the ordering is the only thing the record needs from this fixture — the
		// START of a period is the line the TEST types (ledger.seal), and each turn's
		// index is the number the mock put in the request it is now answering, never a
		// counter of the mock's own.
		//
		// `answersLeft` is how a turn and the request that follows it are kept apart:
		// an accepted turn is answered over TWO rounds (the ask, then the round that
		// carries its tool result), and the ask that comes after it is a NEW request
		// that must be charged to the same period.
		switch {
		case refused:
			// THE CEILING ARRIVED. Its ORDER tells the two periods apart: the first
			// refusal closes the operator's first budget, the second closes the budget
			// their own line renewed. Counting REFUSALS rather than asks is what makes
			// the marker a statement about the loop rather than about how often the mock
			// tried.
			//
			// The refusal is recorded against the ask it REFUSED (the index this mock
			// handed that request), and the outcome is written HERE because this is the
			// round that carries it — see pendingAskTurn.
			ledger.record(periodTurnDecision{turnIndex: int(atomic.LoadInt32(&pendingAskTurn)), accepted: false})
			ledger.serveAsk(int(atomic.LoadInt32(&pendingAskTurn)))
			n := atomic.AddInt32(&refusals, 1)
			if n <= 1 {
				testutil.TextSSE(w, boundPeriodDone)
				return
			}
			testutil.TextSSE(w, boundSecondDone)
		case answeredAlready:
			// The tool ACCEPTED the turn this result belongs to: that is the decision, and
			// it is recorded now, against the ask it answered (the previous ask round).
			ledger.record(periodTurnDecision{turnIndex: int(atomic.LoadInt32(&pendingAskTurn)), accepted: true})
			testutil.TextSSE(w, "ok")
		default:
			n := atomic.AddInt32(&turns, 1)
			atomic.StoreInt32(&pendingAskTurn, n)
			ledger.ask(int(n))
			boundMockSSE(w, boundSelfDrivenLine(int(n)))
		}
	}))
	t.Cleanup(srv.Close)

	pair := chatOpenPtyPair(t, boundCols, boundRows)
	paint := &helpMenuGrid{}
	term, err := lineedit.Open(pair.Slave, paint)
	if err != nil {
		t.Fatalf("open the pty slave as a terminal: %v", err)
	}
	s.pair, s.term, s.paint = pair, term, paint

	in := newChatInputWithSource(term, paint, chatPrompt)
	// THE ANNOUNCEMENT SEAM IS INSTALLED BEFORE THE RUN STARTS, so no notice is missed
	// (chatInput.announce, cmd/agent/lineedit_wire.go). It is additive: the console's own
	// bytes are identical with and without it.
	in.announce = ledger
	in.editor.SetPalette(lineedit.BasicPalette)
	in.editor.SetCompleter(NewSlashCompleter())
	if !in.editor.EnableRegion() {
		t.Fatalf("EnableRegion(pty %dx%d) = false: the bound's acceptance needs the real chat layout", boundCols, boundRows)
	}
	s.in = in

	sl, err := agent.NewSessionLogger(boundModel, "")
	if err != nil {
		t.Fatalf("NewSessionLogger: %v", err)
	}
	t.Cleanup(func() { sl.Close() }) //stylegate:ignore test teardown of a logger this test opened

	ag := &agent.Agent{
		Client:     &deepseek.Client{BaseURL: srv.URL, APIKey: "test-key", HTTPClient: srv.Client()},
		Ctx:        &agent.Context{},
		Model:      boundModel,
		Timeout:    0,
		SessionLog: sl,
	}
	prevFactory := chatAgentFactory
	t.Cleanup(func() { chatAgentFactory = prevFactory })
	chatAgentFactory = func(string) (*agent.Agent, error) { return ag, nil }

	t.Cleanup(func() {
		if s.in != nil {
			s.in.editor.Close()
		}
		// THE SAME ORDER AS THE OTHER REPL FIXTURES, and for the same measured
		// reason (chat_pty_fixture_test.go states it once): the STOP is the
		// terminal's own Close, the join below observes IT, and the descriptors go
		// last. `Stopped()` keeps the by-hand close on the success path from
		// closing the terminal twice.
		if !term.Stopped() {
			_ = term.Close() //stylegate:ignore the test's own terminal teardown
		}
		chatJoinReadLoop(t, s.exited, func() string { return runState(s.exited) })
		_ = pair.Master.Close() //stylegate:ignore the test's own pty master teardown
	})

	go func() {
		defer close(s.exited)
		_ = runChatWithInput([]string{"--tools"}, in)
	}()

	bound := ag.ChatSurfaceLoopBound()
	if bound <= 0 {
		t.Fatalf("the loop's declared bound is %d; this case asserts a ceiling that exists", bound)
	}
	atomic.StoreInt32(&boundShared, int32(bound))

	keyE2ETypeLine(t, s, pair.Master, boundBootstrap)

	// ── PHASE 1: THE CEILING STILL TERMINATES ────────────────────────────────
	// The mock asks for a self-driven turn on every round, so the only thing that can
	// stop it is the bound. The console's announcement that the loop has STOPPED is the
	// CLOSING notice on the last turn ("this is the last self-driven turn of this
	// period (bound N)") — not the CLOSED notice, which announces a line that was
	// typed and then LOST (a `stop`, or a spent counter at the seam): here nothing is
	// lost, the tool simply refuses the NEXT request, so the closing notice is the
	// observable and the refusal is read from the model's own tool result below.
	s.notice(t, "the loop's LAST turn to announce the bound on the console", chatSelfDrivenClosingNotice, bound)
	s.settle(t, "the closing notice to finish drawing")
	// THE MARKER IS READ FROM THE DURABLE STREAM, for the same measured reason
	// PHASE 2's notices are (see streamANSIFree): the mock's refusal is served and
	// the next round begins immediately, so the marker row scrolls OFF a 30-row
	// window while the loop keeps painting. It is not lost — the console wrote it —
	// and the live-grid read is the one that is wrong: measured under concurrent
	// package runs, the wait expired with "PERIOD-TWO-DONE" on screen (the SECOND
	// period's marker, i.e. the response had been served and the marker pushed off
	// the top), which reads as "the refusal never answered" when the answer is on
	// the console. The claim is "the console carried the answer", and the durable
	// stream is the record of exactly that.
	s.sawNotice(t, "the turn that follows the refusal to answer", boundPeriodDone)
	t.Logf("PHASE 1: the console announced the closing turn and the refusal the model hit at the bound %q",
		strings.TrimSpace(fmt.Sprintf(chatSelfDrivenClosingNotice, bound)))

	// THE PERIOD'S CLAIM IS READ OVER THAT PERIOD'S OWN WINDOW IN THE WIRE RECORD.
	//
	// WHAT THIS REPLACES AND WHY (measured — the measurement is the whole reason the
	// assertion changed shape):
	//
	//   - THE WINDOW. PHASE 1 used to compare the mock's own counters `turns`/`refusals`
	//     against a per-period claim, and those counters are scoped to the WHOLE RUN. A
	//     case that has already completed both periods therefore reported the run's
	//     arithmetic against a claim about one period. Measured on the sibling tree: 16
	//     accepted + 2 refused (two periods of 8 + 1) against "8 accepted, exactly 1
	//     refused". The console was correct; the READ was over the wrong window.
	//   - THE NUMBERING. The console numbers a notice `self-driven turn n/bound` with the
	//     SAME text in both periods (the renewed period's first turn is `1 of the bound`
	//     again — that renumbering is what "renewed" MEANS), so a completed run carries
	//     that wording TWICE BY DESIGN. A whole-run count of it read 2 and called a
	//     correct console a defect — measured, whole package on the pre-fix tree:
	//     `chat_surface_bound_e2e_test.go:446: the console announced the self-driven turn
	//     1/8 2 time(s), want exactly 1`. The announcement half of the fix reads such a
	//     notice INSIDE a period's window (see assertRenewedNumbering) for exactly this
	//     reason.
	//
	// THE PERIOD'S WINDOW IS A POSITION IN AN ORDER, NOT A MOMENT IN TIME. The loop PARKS
	// after it closes a period (it refuses every further request until an operator's line
	// renews it — the very behaviour this case measures), so a period's decisions are an
	// ordered slice that is COMPLETE when it is read; waiting for the RENEWAL here
	// instead would deadlock (PHASE 2 is what types it), which is why the numbering is
	// SLICED rather than waited for. The slice is anchored on the wire index the TEST's
	// own record gives the period's first turn, never on a counter of the mock's.
	period1 := s.readPeriod(t, "the first period (the decisions up to and including the refusal that closes it)",
		boundPeriodFirstIndex(boundFirstPeriod, bound), bound)
	s.assertPeriodArithmetic(t, "period 1", boundFirstPeriod, period1, bound)
	// …AND PERIOD 1'S NUMBERED TURNS ARE ANNOUNCED EXACTLY ONCE EACH, read over PERIOD 1'S
	// OWN WINDOW IN THE ANNOUNCEMENT LEDGER — the entries up to and including the closing
	// notice that CLOSES it, which is the console's own statement that the period is
	// complete.
	//
	// WHY A WINDOW AND NOT THE PAINTED COUNT (the reconciliation this file carries): the
	// console numbers a notice `self-driven turn n/bound` with the SAME text in both
	// periods — the renewed period's first turn is `1 of the bound` again, which is what
	// "renewed" MEANS — so a WHOLE-RUN count of that wording is 2 in any run that
	// completes both periods. The pre-fix case counted the PAINTED stream whole-run, so it
	// read 2 for a console behaving exactly as designed and reported the defect at
	// chat_surface_bound_e2e_test.go:446. The painted stream merges two writers (the
	// notice's own band draw and the editor's incremental repaint of the rows it tracks),
	// so it can answer "this wording REACHED the console" and nothing more: it is quoted
	// as evidence inside the failure messages below and is NEVER a verdict here.
	//
	// The closing notice is the boundary the RECORD itself provides (a period is a
	// POSITION in the ordered record, never a moment in time), so the read is complete
	// when it is taken — no race against the loop, which is exactly what the painted count
	// could not offer.
	firstClosing := flattened(fmt.Sprintf(chatSelfDrivenClosingNotice, bound))
	closingDeadline := time.Now().Add(boundWait)
	for time.Now().Before(closingDeadline) {
		if len(s.ledger.entriesContaining(firstClosing)) > 0 {
			break
		}
		time.Sleep(boundStep) //stylegate:allow-sleep bounded poll (boundWait) for period 1's closing announcement
	}
	if len(s.ledger.entriesContaining(firstClosing)) == 0 {
		t.Fatalf("the console announced no closing turn for the first period within %s, so the period's own window in the announcement ledger "+
			"cannot be located — the numbered turns below are read up to that boundary and it never arrived\n%v\n--- durable stream tail ---\n%s",
			boundWait, s.ledger.entries(), s.paint.streamTail())
	}
	for n := boundPeriodFirstTurn; n <= bound; n++ {
		want := flattened(fmt.Sprintf(chatSelfDrivenTurnNotice, n, bound))
		if got := s.ledger.countBefore(firstClosing, want); got != 1 {
			t.Fatalf("in the first period (the announcements up to and including the closing notice), self-driven turn %d/%d was announced "+
				"%d time(s), want exactly 1 — the loop numbers each accepted turn once, and the renewed period's identical %d/%d is announced "+
				"LATER, after the operator's own line, so it is not in this window\npainted evidence (NOT a verdict — it also counts a repaint "+
				"of the same row): %d\n%v\n--- durable stream tail ---\n%s",
				n, bound, got, boundPeriodFirstTurn, bound,
				strings.Count(s.paint.streamANSIFree(), want), s.ledger.entries(), s.paint.streamTail())
		}
	}
	// …AND NOTHING PAST THE CEILING — read over the ledger, and over the WHOLE run on
	// purpose: a number outside 1..bound is not a statement about a period, it is the
	// ceiling itself being broken, and it must be zero for every period at once. (The
	// per-period read above pins each period's own numbering to the bound consecutive
	// turns starting at 1, so an overshoot inside a period is already red there; this is
	// the second, coarser statement.)
	if over := ledger.count(flattened(fmt.Sprintf(chatSelfDrivenTurnNotice, bound+1, bound))); over != 0 {
		t.Fatalf("the console announced a self-driven turn %d/%d %d time(s), past the bound of %d — the loop's own numbering must stop at the ceiling\n%v\n--- durable stream tail ---\n%s",
			bound+1, bound, over, bound, ledger.decisions(), s.paint.streamTail())
	}
	// THE MOCK'S ASKS ARE RECONCILED WITH ITS OWN DECISION RECORD — a statement about the
	// FIXTURE, never about the bound: each ask must be paired with a decision, and the two
	// sides of an ask must name the SAME outcome. It is deliberately not the per-period
	// claim any more (that reads the period's own window above), so a fixture-side
	// miscount can no longer be reported as a bound violation — the defect this task
	// removes.
	//
	// The RUN-SCOPED form of this very check is what the whole-package run found next
	// (measured 2026-10-02: it compared two totals, demanded `asks == decisions`, and read
	// `the mock offered 12 self-driven request(s) but the wire record holds 10 decision(s)`
	// for a fixture behaving exactly as designed — the same mistake in a third instrument).
	// The two totals legitimately differ, and NOT by a fixed amount: the case's reads are
	// taken while the console races ahead, so an ask whose result round has not been served
	// yet is undecided by construction, and how many are in flight is a statement about how
	// far the RUN has got. Pairing by turn index asks the same question without borrowing
	// the run's progress INTO it.
	if mismatched, unpaired := ledger.askReconciliation(); len(mismatched) > 0 || len(unpaired) > 0 {
		t.Fatalf("the mock's asks and its own decision record disagree: turn(s) %v carry a decision for an ask that was never made, "+
			"and turn(s) %v carry a different outcome than the ask they belong to — the fixture must describe one wire\n%v",
			unpaired, mismatched, ledger.decisions())
	}

	// ── PHASE 2: THE OPERATOR'S OWN LINE RENEWS IT ───────────────────────────
	// A line written on the pty MASTER — the carrier's read arm, the one path a
	// model-written line can never take (the tool's `type` arrives on the seam, not
	// here). This is the ONLY renewal, and it is announced on the console.
	//
	// These two notices are read from the DURABLE STREAM rather than the live grid, and
	// the difference is honest rather than convenient: the mock is racing to the bound at
	// this point, so the renewed period's first turn is announced and then SCROLLS AWAY
	// before a 30-row grid retains it. The claim being made is "the console announced
	// it", which is a statement about what was written; PHASE 1's notice, read live, is
	// the case's "the operator can read it as it happens" check.
	keyE2ETypeLine(t, s, pair.Master, boundRenewedLine)
	// THE RENEWAL IS ANNOUNCED EXACTLY ONCE, read at the announcement seam: a whole-run
	// count of this notice would mean something only if the renewal could not happen
	// again, and the claim is the opposite one — one renewal per operator line, so the
	// count over the run IS the per-event claim here (and the painted stream, which also
	// carries the editor's repaint, is quoted as evidence beside it).
	s.sawNoticeOnce(t, "the console's renewal notice (one per operator line)", chatSelfDrivenRenewalNotice, bound)
	// AND THE BUDGET REALLY CAME BACK, which is the deliverable: the reopened period is
	// numbered 1 of the bound AGAIN — the renumbering that made the old whole-run count
	// red. It is read from the LEDGER (an event), not from the painted stream (a write
	// log), and over the position the renewal opened (entries strictly after it): the
	// first period's own "1 of the bound" sits BEFORE the renewal, so a whole-run count
	// could not have made this statement at all.
	renewals := ledger.entriesContaining(flattened(fmt.Sprintf(chatSelfDrivenRenewalNotice, bound)))
	if len(renewals) == 0 {
		t.Fatalf("the renewal notice was painted but the announcement ledger holds no renewal entry, so the renewed period's window cannot be located\n%v\n--- durable stream tail ---\n%s",
			ledger.entries(), s.paint.streamTail())
	}
	s.assertRenewedNumbering(t, renewals[0], bound)

	// ── PHASE 3: THE RENEWED PERIOD TERMINATES TOO ───────────────────────────
	// A renewable budget is still a bounded one: the second period runs out exactly as
	// the first did, and says so — read from the DURABLE STREAM for the reason PHASE 1
	// states (the mock races to the bound, so the marker leaves the live window).
	s.sawNotice(t, "the renewed period to reach the bound as well", boundSecondDone)
	s.settle(t, "the second closure to finish drawing")
	// THE RENEWED PERIOD'S OWN ARITHMETIC, over ITS OWN window: exactly the bound
	// accepted (numbered 1..bound again, which is what "renewed" MEANS) and exactly one
	// refusal at the ceiling. This is the read the pre-fix assertion could not make: its
	// per-period claim was taken over both periods' totals by the time it ran, so it
	// reported the RUN's arithmetic (16 accepted + 2 refused against a claim of 8 + 1)
	// while the console was behaving exactly as designed.
	period2 := s.readPeriod(t, "the renewed period (the decisions after the operator's line, up to the refusal that closes it)",
		boundPeriodFirstIndex(boundRenewedPeriod, bound), bound)
	s.assertPeriodArithmetic(t, "the renewed period", boundRenewedPeriod, period2, bound)
	t.Logf("PERIODS: period %d accepted %d turn(s) numbered %d..%d (wire %d..%d) with %d refusal(s); period %d accepted %d numbered %d..%d "+
		"(wire %d..%d) with %d refusal(s) — each read over ITS OWN window, so the two periods' totals never enter one claim",
		boundFirstPeriod, period1.accepted, period1.first, period1.last, period1.wireFirst, period1.wireLast, period1.refused,
		boundRenewedPeriod, period2.accepted, period2.first, period2.last, period2.wireFirst, period2.wireLast, period2.refused)

	// THE KEY'S OWN DELIVERY IS NOW REPORTED, and it is the fact the closeout wait
	// could not previously see. exitAfterInterrupt reports the RUN's state (the
	// goroutine, the model rounds); on its own that reads identically for "the key
	// was never delivered" and "the key was delivered and did not end the session",
	// which are opposite defects with opposite fixes. InjectResult.ReadByEditor is
	// the injector's own observation of the read loop TAKING the key (see
	// inject.go awaitDrain — a poll, and a timeout is reported as false rather than
	// assumed), so it is quoted here as a MEASUREMENT, and injectLatency is how long
	// the queue waited before the read loop took it. Measured on this host at the
	// prompt (2026-10-02, 12 consecutive runs of this case): ReadByEditor=true in
	// 5-27ms, the run leaving within 1ms of the read — i.e. at the prompt the key is
	// taken immediately and the session ends on the same iteration.
	// THE INJECTION-TIME STATE IS LOGGED FIRST, so a failure can be read without a
	// second run: whether a turn was in flight when the key arrived (the editor's
	// at-a-turn meaning of ctrl-c) and what the run's two progress counters read at
	// that instant are exactly the facts the closeout's verdict is about.
	t.Logf("ctrl-c injection-time state: minTurnActive=%v modelRoundsServed=%d bytesPainted=%d",
		s.in.editor.MinTurnActive(), roundsServed(), s.paint.painted())
	injectedAt := time.Now()
	res := s.in.injector.InjectKey(lineedit.KeyNameCtrlC)
	injectLatency := time.Since(injectedAt)
	t.Logf("ctrl-c injected: accepted=%v readByEditor=%v pending=%d took=%v",
		res.Accepted, res.ReadByEditor, res.Pending, injectLatency.Round(time.Millisecond))
	if !res.Accepted {
		t.Fatalf("injecting ctrl-c refused: %s", res.Why)
	}
	// WAIT FOR THE RUN TO LEAVE — see exitAfterInterrupt, which names the mechanism
	// (the key claims the turn in flight, so a closeout comes first), reports the
	// run's own state on failure instead of a bare "did not finish", and explains
	// why a second ctrl-c is NOT the fix (it would be a second key on the same seam
	// and would change what this case asserts).
	s.exitAfterInterrupt(t, roundsServed)

	t.Logf("BOUND: %d model round(s); the self-driven loop stopped at the absolute ceiling TWICE (once before the operator's line, once after it), and the console announced the closure, the renewal and the re-numbered turn", roundsServed())
}
