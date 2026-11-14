package agent

// chat_close_signal_category_test.go — the regression guard for the
// "disappeared with no explanation" report (epic chat-vanish-trace, session
// ab9e0023, 2026-10-21).
//
// THE MEASURED DEFECT. A chat session ended by a CATCHABLE signal — a terminal
// teardown's SIGHUP, a supervisor's SIGTERM — unwinds through the carrier's own
// close-out with a non-zero exit code, so CloseChatSession collects a failure
// record. Every arm of collectErrorRecord keys on a RUN ERROR, and a signal death
// has none: the record fell through to `category: "unknown"` with an empty
// error_body, written beside an orderly, deliberate close-out. Reproduced on a
// pty against this branch's own binary before the fix:
//
//	$ gopi chat            # real pty, then: kill -HUP <pid>
//	[gopi] hangup received — finalizing chat session…   # on screen: orderly
//	bye.
//	$ echo $?              # 1
//	$ cat session.dump.json | grep category
//	    "category": "unknown",     ← the run FAILED, apparently, for no reason
//
// and the session directory held NO signal-provenance record at all, because the
// chat carrier never performed the record-filed handshake every other claiming
// door performs (that half is pinned end to end in the testsuite:
// TestChatPTY_HangupIsTraced_NOTVanished).
//
// WHAT THIS FILE PINS, and it is deliberately the SMALLEST sample that can
// distinguish the two behaviours (Rule 10.4): the collector's decision, called
// through its real entry point with the carrier's declaration set exactly the way
// the carrier sets it.

import (
	"strings"
	"testing"

	"github.com/nickhuang99/gopi/internal/deepseek"
)

// The signal spellings the guard exercises, as signalrec.SignalName renders them
// (internal/signalrec's classifySignal — this package never re-spells a signal
// name, Rule 14; the test uses the literal OUTPUT so a drift in the spelling
// shows up here as a failure rather than being mirrored silently).
const (
	testSignalHangup = "SIGHUP"
	testSignalTerm   = "SIGTERM"
)

// TestCollectErrorRecord_SignalDeathIsItsOwnCategory is the red proof: the
// carrier declared a signal and there is NO run error, so the record must say
// "signal" and NAME it — not "unknown", and not an empty body.
func TestCollectErrorRecord_SignalDeathIsItsOwnCategory(t *testing.T) {
	a := &Agent{CloseSignal: testSignalHangup}
	er := a.collectErrorRecord(nil, 0, 0, 5.99)
	if er == nil {
		t.Fatal("collectErrorRecord returned nil for a signal-ended session")
	}
	if er.Category != string(deepseek.CategorySignal) {
		t.Fatalf("category = %q, want %q — a handled signal is a KNOWN cause, not an unrecognised one",
			er.Category, deepseek.CategorySignal)
	}
	if strings.TrimSpace(er.ErrorBody) == "" {
		t.Fatal("error_body is empty — the record does not name what ended the session")
	}
	if !strings.Contains(er.ErrorBody, testSignalHangup) {
		t.Fatalf("error_body does not name the signal %q: %q", testSignalHangup, er.ErrorBody)
	}
	// The sentence must say the run did NOT fail: that half is the whole point.
	// "unknown" made an orderly teardown look like a provider failure.
	if !strings.Contains(er.ErrorBody, "did not fail") {
		t.Fatalf("error_body does not state that the run did not fail: %q", er.ErrorBody)
	}
	if er.RecoverableHint {
		t.Fatal("recoverable_hint is true — there is nothing wrong with the run to retry")
	}
}

// TestCollectErrorRecord_NoSignalDeclarationKeepsTheOldAnswer is the other side
// of the same predicate: a session that declared NO signal must not acquire the
// category. Without this arm, "always stamp signal" would pass the test above.
func TestCollectErrorRecord_NoSignalDeclarationKeepsTheOldAnswer(t *testing.T) {
	a := &Agent{}
	er := a.collectErrorRecord(nil, 0, 0, 1.0)
	if er == nil {
		return // a clean exit collecting nothing is also acceptable
	}
	if er.Category == string(deepseek.CategorySignal) {
		t.Fatalf("category = %q for a session that declared no signal", er.Category)
	}
}

// TestCollectErrorRecord_RealFailureOutranksTheSignal is the precedence arm, and
// it is not hypothetical: a signal often arrives BECAUSE the run was already
// failing (a supervisor kills a hung turn). Reporting "signal" there would hide
// the cause worth fixing, so a classified failure must survive the declaration.
func TestCollectErrorRecord_RealFailureOutranksTheSignal(t *testing.T) {
	a := &Agent{CloseSignal: testSignalTerm}
	er := a.collectErrorRecord(errTestRunVendorFailure, 3, 100, 12.5)
	if er == nil {
		t.Fatal("collectErrorRecord returned nil — the run error was dropped")
	}
	if er.Category == string(deepseek.CategorySignal) {
		t.Fatalf("category = %q — the signal arm overwrote a REAL classified failure (%q)", er.Category, er.ErrorBody)
	}
}

// errTestRunVendorFailure is a run error the classifier has a real arm for: the
// provider's own overload wording, the measured `vendor` class
// (crashdump_classify_test.go's table). It is used here only to give the
// precedence test something the collector will actually classify.
var errTestRunVendorFailure = errString("stream error: overloaded")

// errString is a minimal error so the fixture needs no dependency.
type errString string

func (e errString) Error() string { return string(e) }
