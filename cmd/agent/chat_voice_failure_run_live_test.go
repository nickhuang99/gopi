// chat_voice_failure_run_live_test.go — the LIVE arm obeys the SAME rule about a
// failing engine as the window arm (task 01a121fc, deliverable D).
//
// WHY IT IS A SEPARATE CASE AND NOT A SECOND ARM OF THE WINDOW CASE. `/voice`
// has two loops (loop and liveLoop) and the defect was in BOTH: each called
// m.fail(err) on any error. A fix applied to one loop would leave the other
// closing the microphone on a single refusal — and the live arm is the one where
// a transient failure is MOST likely, because every utterance is a fresh network
// session against a billed streaming service.
//
// THE ONE PREDICATE IS WHAT MAKES THEM AGREE (voiceChatFailuresGiveUp over the
// shared failStreak), so this case asserts the rule on the live arm rather than
// re-asserting the window arm's arithmetic: a run of N-1 failures leaves the mode
// ON, and the run that reaches N leaves it OFF.
package main

import (
	"strings"
	"testing"
)

// voiceLiveFailureRunErr is the live arm's scripted refusal: one streaming
// session the service refused (a reconnect, a timeout). It is named (Rule 1) and
// deliberately unlike doubaostream.ErrSessionFinished, which is the ordinary
// ending rather than a failure.
var voiceLiveFailureRunErr = errVoiceTest("the streaming session was refused by the service")

// voiceLiveFailureRunMode builds the live-loop harness the cases below drive: the
// loop's generation, done channel and session double, exactly as the existing
// live cases do (a nil loopDone would hang rather than fail, which is not
// evidence).
func voiceLiveFailureRunMode(rec *voiceChatRecorderScriptedTest) (*voiceChatMode, *voiceLiveSessionTest) {
	m := liveModeForTest()
	m.recorder = rec
	sess := &voiceLiveSessionTest{}
	m.sess = sess
	m.state.started = 1
	m.state.loopDone = make(chan struct{})
	return m, sess
}

// TestVoiceLiveLoop_OneFailedUtteranceDoesNotEndTheMode is the live arm's half of
// the defect: utterance 1 fails, utterance 2 answers (and ends the capture), and
// the mode must have KEPT LISTENING between them.
//
// RED ON THE PRE-FIX SHAPE: the first error leaves the mode, so the recorder is
// asked for ONE utterance and the retry row never appears.
func TestVoiceLiveLoop_OneFailedUtteranceDoesNotEndTheMode(t *testing.T) {
	rec := &voiceChatRecorderScriptedTest{errs: []error{voiceLiveFailureRunErr, nil}}
	m, sess := voiceLiveFailureRunMode(rec)
	runLoopBounded(t, m)

	if got := rec.callCount(); got != 2 {
		t.Fatalf("the loop ran %d utterance(s), want 2 — one refused streaming session must not end the mode "+
			"(the operator is mid-dictation)", got)
	}
	if sess.contains(chatVoiceLeftByFailed) {
		t.Errorf("a single refused session was reported as the FAILED exit; rows were:\n%s", sess.rows())
	}
	// BOTH SIDES ARE FLATTENED: the row's own text carries spaces, so a flattened
	// expectation is only a substring of a flattened read.
	if !strings.Contains(voiceFlatten(sess.rows()), voiceFlatten(voiceRetryRow(voiceLiveFailureRunErr, 1))) {
		t.Errorf("the retried utterance was not reported through the voice notice road; rows were:\n%s", sess.rows())
	}
}

// TestVoiceLiveLoop_TheRunOfFailuresEndsTheMode is the other side: failures that
// do not stop must still end the mode at voiceChatMaxConsecutiveFailures, never
// never.
func TestVoiceLiveLoop_TheRunOfFailuresEndsTheMode(t *testing.T) {
	run := make([]error, 0, voiceChatMaxConsecutiveFailures)
	for i := 0; i < voiceChatMaxConsecutiveFailures; i++ {
		run = append(run, voiceLiveFailureRunErr)
	}
	rec := &voiceChatRecorderScriptedTest{errs: run}
	m, sess := voiceLiveFailureRunMode(rec)
	runLoopBounded(t, m)

	if got := rec.callCount(); got != voiceChatMaxConsecutiveFailures {
		t.Fatalf("the loop ran %d utterance(s), want exactly %d — the run must end the mode at the limit, not before and not after",
			got, voiceChatMaxConsecutiveFailures)
	}
	if !sess.contains(chatVoiceLeftByFailed) {
		t.Errorf("the run of %d failures did not end the mode; rows were:\n%s", voiceChatMaxConsecutiveFailures, sess.rows())
	}
	// THE EARLIER FAILURES ARE STILL REPORTED: a run that only spoke at its end
	// would leave the operator watching a silent mode for N sessions (Rule 15).
	if !sess.contains("voice round FAILED (1 of") {
		t.Errorf("the first failure of the run produced no row; rows were:\n%s", sess.rows())
	}
}
