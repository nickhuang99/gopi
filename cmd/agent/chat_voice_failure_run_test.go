// chat_voice_failure_run_test.go — ONE failed window must not close the
// microphone (task 01a121fc, deliverable D).
//
// WHAT WAS WRONG. Every failure path of both loops called m.fail(err), which
// printed chatVoiceErrorNote and left the mode at once. The failure modes this
// mode actually meets are TRANSIENT — a cloud upload that refused one window, a
// local engine that missed one call — so a single bad window ended an operator's
// whole dictation session and told them it was fatal.
//
// THE RULE UNDER TEST is stated in one place and read by both arms:
// voiceChatMaxConsecutiveFailures failed windows IN A ROW end the mode
// (voiceChatFailuresGiveUp); every earlier failure is reported through the
// existing notice road (chatVoiceRoundRetryNote) and the mode keeps listening. A
// round that comes back at all — silence included — clears the run, so the count
// can only accumulate while the engine is really down.
//
// THE JUDGE IS THE GRID for the window arm (Rule 10.2): the assertions read back
// the cells the operator's terminal shows. The live arm's cases read the session
// double's own rows, because there is no terminal in that harness (the live
// loop's carrier is asserted separately by chat_voice_live_pty_test.go).
package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

const (
	// voiceFailureRunWait bounds a wait for a run of paced rounds to play out.
	// It is WIDER than the shared voicePtyWait on purpose: each retried window
	// costs the ordinary pacing gap (voiceChatWindowPauseSeconds), so a run of
	// voiceChatMaxConsecutiveFailures windows cannot finish inside that bound —
	// a wait that short would fail the case for its own arithmetic rather than
	// for the behaviour.
	voiceFailureRunWait = 30 * time.Second
	// voiceFailureRunStep is that wait's polling interval.
	voiceFailureRunStep = 5 * time.Millisecond
)

// voiceFailureRunErr is the scripted engine refusal: a window the engine could
// not answer. It is named (Rule 1) and deliberately unlike errVoiceStreamClosed,
// which is the SENTINEL exit rather than a failure.
var voiceFailureRunErr = errors.New("the engine refused the window")

// waitVoiceRounds polls until at least n rounds have been recorded, or the bound
// expires. It returns what it saw rather than failing, so each case can assert
// about the COUNT it needed instead of about the wait.
func waitVoiceRounds(rec *scriptedRecorder, n int, bound time.Duration) int {
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) && rec.roundsRun() < n {
		time.Sleep(voiceFailureRunStep) //stylegate:allow-sleep bounded poll (voiceFailureRunWait) for the paced rounds
	}
	return rec.roundsRun()
}

// waitForBound is voicePtyGrid.waitFor with a CALLER-CHOSEN bound: a case about a
// RUN of paced rounds needs a bound wider than the shared voicePtyWait, because
// each retried window costs the ordinary pacing gap rather than nothing.
func (h *voicePtyGrid) waitForBound(t *testing.T, what string, bound time.Duration, cond func(string) bool) {
	t.Helper()
	deadline := time.Now().Add(bound)
	for time.Now().Before(deadline) {
		if cond(h.rows()) {
			return
		}
		time.Sleep(voiceFailureRunStep) //stylegate:allow-sleep bounded poll (bound) for the paced rounds to paint
	}
	t.Fatalf("timed out waiting for %s\n--- grid ---\n%s", what, h.rows())
}

// voiceRetryRow is the retry note as the operator sees it, composed from the
// PRODUCTION template plus the case's own numbers. The wording is the code's (a
// case that typed its own copy would keep passing after the note moved); the
// NUMBERS are the case's, which is what makes this an assertion about the count
// rather than a tautology.
func voiceRetryRow(err error, run int) string {
	return fmt.Sprintf(chatVoiceRoundRetryNote, run, voiceChatMaxConsecutiveFailures,
		err, voiceChatMaxConsecutiveFailures-run)
}

// TestChatVoice_OneFailedWindowDoesNotEndTheMode is the defect case: window 1
// fails, window 2 succeeds, and the mode must STILL BE ON with the second
// window's text on the line.
//
// RED ON THE PRE-FIX SHAPE (m.fail on any error): the first failure leaves the
// mode, so the recorder is never asked for a second round, no text lands, and the
// grid carries the FAILED row instead of the retry row.
func TestChatVoice_OneFailedWindowDoesNotEndTheMode(t *testing.T) {
	rec := &scriptedRecorder{
		desc:      "test mic",
		roundErrs: []error{voiceFailureRunErr, nil},
		texts:     []string{"hello"},
	}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	t.Cleanup(func() { h.mode.shutdown() })

	// THE SECOND WINDOW IS THE POINT, and it is asserted FIRST because nothing
	// else in this case can happen without it: the recorder is asked for round 2
	// only if the first failure did not leave the mode. (The script's second entry
	// is nil, so round 2 answers with `hello`.)
	if got := waitVoiceRounds(rec, 2, voiceFailureRunWait); got < 2 {
		t.Fatalf("the recorder was asked for %d round(s): a single failed window ended the mode — "+
			"the transient refusal this rule exists for is treated as fatal\n--- grid ---\n%s", got, h.rows())
	}
	// THE RETRY REACHES THE LINE, which is what makes "still listening" a fact
	// rather than a sentence: the window after the failure was transcribed and
	// appended.
	h.waitFor(t, "the second window's transcript on the line", func(rows string) bool {
		return strings.Contains(rows, "hello")
	})
	if !h.mode.active() {
		t.Fatalf("the mode is OFF after ONE failed window\n--- grid ---\n%s", h.rows())
	}
	if got := rec.roundsRun(); got < 2 {
		t.Fatalf("the loop ran %d round(s) — the failed window was not retried", got)
	}
	rows := h.rows()
	if strings.Contains(rows, "voice mode FAILED") {
		t.Errorf("a single failed window reported the mode as FAILED — the run had not reached its limit\n--- grid ---\n%s", rows)
	}
	if !strings.Contains(voiceFlatten(rows), voiceFlatten(voiceRetryRow(voiceFailureRunErr, 1))) {
		t.Errorf("the retried failure was not reported through the voice notice road, "+
			"or it did not say which failure of how many it was\nwant: %s\n--- grid ---\n%s", voiceRetryRow(voiceFailureRunErr, 1), rows)
	}
}

// TestChatVoice_TheRunOfFailuresEndsTheMode is the other half of the same rule,
// and it is what stops the fix from being "never give up": failures that do NOT
// stop must end the mode at voiceChatMaxConsecutiveFailures, not later and not
// never.
//
// IT IS DRIVEN BY THE CONSTANT, not by a literal 3: the case asserts the rule
// (the Nth consecutive failure ends it, and the N-1 before it do not), so a
// future change to N keeps the assertion true rather than turning it red for the
// wrong reason.
func TestChatVoice_TheRunOfFailuresEndsTheMode(t *testing.T) {
	rec := &scriptedRecorder{desc: "test mic", roundErr: voiceFailureRunErr}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	t.Cleanup(func() { h.mode.shutdown() })

	// THE WAIT READS THE SAME TWO FACTS THE ASSERTIONS BELOW READ (task
	// 01a12c12), and that is the fix. fail() QUEUES the FAILED row (m.notice →
	// the editor's band seam, which the READ LOOP paints on its own goroutine)
	// and only THEN leaves (m.leave → shutdown → `on = false`). The queue is a
	// hand-off, so "the row is on the grid" and "the state has flipped" are two
	// different instants reached by two different goroutines — a wait on the ROW
	// alone can return inside that gap, and the reading taken immediately
	// afterwards then reports a mode that has simply not switched off yet. That
	// is the scheduler's order, not the rule under test: waiting for both facts
	// together, inside one bound, is what makes the assertion about the rule.
	//
	// MERGE NOTE (task 01a12c70): the archived bandwait branch (task/01a12c43,
	// tip 386215e51) edited this same case while 01a12c12 was still unmerged, so
	// its wording carried the caveat "that branch is unmerged and this task's
	// acceptance runs on both cases". 01a12c12 has since landed here
	// (ff3b8c5b4), which makes master's wording the accurate one and the caveat
	// stale: the assertion below is the SAME fix on both sides, so master's text
	// is kept verbatim and the branch's contribution to this case — its reason
	// for requiring the run's end to be read as ONE bound — is recorded here.
	// The bandwait branch's assertion of its own is
	// internal/lineedit/bandwait_owner_test.go, added by this merge.
	h.waitForBound(t, "the run's end — the FAILED row AND the mode off", voiceFailureRunWait, func(rows string) bool {
		return strings.Contains(rows, "voice mode FAILED") && !h.mode.active()
	})
	if h.mode.active() {
		t.Error("the mode is still ON after the run of failures reached its limit — " +
			"a device that is gone would keep a status row claiming a microphone nobody reads")
	}
	if got := rec.roundsRun(); got != voiceChatMaxConsecutiveFailures {
		t.Errorf("the loop ran %d round(s), want exactly %d — a failing engine must not be retried past the limit "+
			"(and must not give up before it)", got, voiceChatMaxConsecutiveFailures)
	}
	rows := voiceFlatten(h.rows())
	// EVERY FAILURE BEFORE THE LAST ONE IS REPORTED, and the last one is the
	// FAILED row instead: the N-1 retry rows are the evidence that the earlier
	// failures were neither fatal nor silent (Rule 15).
	for run := 1; run < voiceChatMaxConsecutiveFailures; run++ {
		if !strings.Contains(rows, voiceFlatten(voiceRetryRow(voiceFailureRunErr, run))) {
			t.Errorf("failure %d of %d was not reported as a retried window\nwant: %s\n--- grid ---\n%s",
				run, voiceChatMaxConsecutiveFailures, voiceRetryRow(voiceFailureRunErr, run), h.rows())
		}
	}
	if got, want := strings.Count(rows, voiceFlatten("voice mode FAILED")), 1; got != want {
		t.Errorf("the grid carries %d FAILED row(s), want %d — exactly the last failure of the run is fatal",
			got, want)
	}
}

// TestChatVoice_ASuccessClearsTheRunOfFailures is the RESET half, and it is the
// assertion that makes the count "consecutive" rather than "cumulative": the
// script fails N-1 times, succeeds, fails N-1 times again, and succeeds — so a
// mode that merely totalled its failures would be off by the second run, while
// one that counts a RUN is still listening.
//
// THE FIRST SUCCESS IS A SILENT WINDOW ON PURPOSE (no scripted text): an empty
// transcript is the engine ANSWERING with nothing, which is not a failure, and a
// rule that only cleared on a transcript would leave a quiet room counting
// towards the limit.
func TestChatVoice_ASuccessClearsTheRunOfFailures(t *testing.T) {
	script := make([]error, 0, 2*voiceChatMaxConsecutiveFailures)
	for i := 0; i < voiceChatMaxConsecutiveFailures-1; i++ {
		script = append(script, voiceFailureRunErr)
	}
	script = append(script, nil) // the quiet window: no text, no error
	for i := 0; i < voiceChatMaxConsecutiveFailures-1; i++ {
		script = append(script, voiceFailureRunErr)
	}
	script = append(script, nil)
	rec := &scriptedRecorder{desc: "test mic", roundErrs: script}
	h := newVoicePtyGrid(t, rec)
	h.mode.resolveOpts = voiceChatTestOptions
	h.mode.depsMissing = func(*voiceOptions) string { return "" }
	h.enter(t)
	t.Cleanup(func() { h.mode.shutdown() })

	// THE SCRIPT IS PLAYED OUT IN FULL: every entry is one round, so waiting for
	// the last one is what proves the quiet window in the middle reset the run
	// (without the reset the mode would have left at round N, far short of the
	// script's length).
	if got := waitVoiceRounds(rec, len(script), voiceFailureRunWait); got < len(script) {
		t.Fatalf("only %d of the script's %d rounds ran: the mode gave up inside a run that a quiet window had already cleared", got, len(script))
	}
	if !h.mode.active() {
		t.Fatalf("the mode is OFF though no run of %d consecutive failures ever happened\n--- grid ---\n%s",
			voiceChatMaxConsecutiveFailures, h.rows())
	}
	if strings.Contains(h.rows(), "voice mode FAILED") {
		t.Errorf("a run interrupted by a successful window was reported as fatal\n--- grid ---\n%s", h.rows())
	}
}
