package lineedit

import (
	"bytes"
	"strings"
	"testing"
)

// degrade_record_test.go — A DEGRADATION MUST SURVIVE THE SCREEN THAT SHOWED IT.
//
// Measured (task 01a18990): the editor's degradations went to errOut only, and
// cmd/agent binds errOut to os.Stderr while a chat run's console.log receives
// the AGENT's logger — so `grep 'read loop is stalled' console.log` returned 0
// after a real stall. The report of the defect was the one artifact that could
// not contain it, which is a silent failure of the REPORT (Rule 15): the next
// reader concludes it never happened.
//
// The sink is a RECORD, never the flow seam: a degradation that queued a band row
// would re-enter the very wait it reports on.

// TestDegradation_ReachesBothTheScreenAndTheRecord pins the two destinations and
// that they receive the SAME sentence — composed once, so they cannot disagree
// about what happened.
func TestDegradation_ReachesBothTheScreenAndTheRecord(t *testing.T) {
	var screen, record bytes.Buffer
	ed := NewChatEditor(&scriptedSource{holdAtEnd: true}, &recordingWriter{}, promptForTest, nil, &screen, nil)
	t.Cleanup(ed.Close)
	ed.SetDiagSink(func(s string) { record.WriteString(s) })

	ed.reportAnswerWaitFailure()

	if screen.Len() == 0 {
		t.Error("nothing was written to the operator's screen: the historic behaviour must not be lost")
	}
	if record.Len() == 0 {
		t.Fatal("nothing was written to the RECORD: a degradation whose only reader is the screen cannot be used " +
			"to diagnose the screen — this is the measured defect (console.log carried 0 stalls while one happened)")
	}
	if screen.String() != record.String() {
		t.Errorf("screen and record disagree:\n screen: %q\n record: %q", screen.String(), record.String())
	}
	if !strings.Contains(record.String(), "read loop is stalled") {
		t.Errorf("the recorded sentence does not name the failure: %q", record.String())
	}
}

// TestDegradation_NoSinkKeepsTheHistoricBehaviour is the control arm: every
// caller with no record to write (every non-chat caller, and every test built
// before this seam) must behave exactly as before.
func TestDegradation_NoSinkKeepsTheHistoricBehaviour(t *testing.T) {
	var screen bytes.Buffer
	ed := NewChatEditor(&scriptedSource{holdAtEnd: true}, &recordingWriter{}, promptForTest, nil, &screen, nil)
	t.Cleanup(ed.Close)

	ed.reportAnswerWaitFailure()

	if screen.Len() == 0 {
		t.Fatal("with no sink installed the sentence must still reach errOut")
	}
}

// TestDegradation_NilErrOutStillReachesTheRecord is the arm that makes the sink
// worth having: a caller that wired NO stream (errOut nil — a headless harness, a
// test) still gets its degradation recorded instead of dropped on the floor.
func TestDegradation_NilErrOutStillReachesTheRecord(t *testing.T) {
	var record bytes.Buffer
	ed := NewChatEditor(&scriptedSource{holdAtEnd: true}, &recordingWriter{}, promptForTest, nil, nil, nil)
	t.Cleanup(ed.Close)
	ed.SetDiagSink(func(s string) { record.WriteString(s) })

	ed.reportAnswerWaitFailure()

	if record.Len() == 0 {
		t.Fatal("a degradation was dropped: errOut was nil and the sink was the only destination left")
	}
}

// TestWarn_AlsoReachesTheRecord is the arm task 01a1a0ed added, and it is the
// one the existing three could not catch: they exercise degradation() directly,
// while the editor's SIX user-facing warnings all call warn(). Measured
// 2026-11-03: `warn` wrote errOut and nothing else, so the sentence the
// operator was told on screen —
//
//	[gopi] editor: read an Enter as pasted content, not as a submit: the
//	previous read delivered 6 key(s) at once and this Enter arrived 0s later …
//
// — appeared in NO file under the session directory: `grep -r 'read an Enter as
// pasted' /home/nick/.gopi/sessions/e1e22e9f-…` returned 0 hits while the operator had
// just been shown it. The sink is the ONE road that exists so a degradation
// outlives the terminal that showed it, and warn() was not on it.
func TestWarn_AlsoReachesTheRecord(t *testing.T) {
	var screen, record bytes.Buffer
	ed := NewChatEditor(&scriptedSource{holdAtEnd: true}, &recordingWriter{}, promptForTest, nil, &screen, nil)
	t.Cleanup(ed.Close)
	ed.SetDiagSink(func(s string) { record.WriteString(s) })

	const msg = "a warn()-class degradation"
	ed.warn(msg)

	want := "[gopi] editor: " + msg + "\n"
	if screen.String() != want {
		t.Errorf("the screen's bytes changed:\n got: %q\nwant: %q", screen.String(), want)
	}
	if record.String() != want {
		t.Errorf("the record does not carry the warning:\n got: %q\nwant: %q", record.String(), want)
	}
}

// TestWarn_NilErrOutStillReachesTheRecord is the same control the degradation arm
// already has: a caller that wired no stream must still have its warning
// recorded rather than dropped (Rule 15 — an announce that reaches nobody is a
// silent failure wearing a message).
func TestWarn_NilErrOutStillReachesTheRecord(t *testing.T) {
	var record bytes.Buffer
	ed := NewChatEditor(&scriptedSource{holdAtEnd: true}, &recordingWriter{}, promptForTest, nil, nil, nil)
	t.Cleanup(ed.Close)
	ed.SetDiagSink(func(s string) { record.WriteString(s) })

	ed.warn("dropped?")

	if record.Len() == 0 {
		t.Fatal("a warning was dropped: errOut was nil and the sink was the only destination left")
	}
}
