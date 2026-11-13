package agent

// interrupt_source_vocabulary_test.go — the CLOSED vocabularies behind the
// turn_interrupted witness, and the one property a future edit is most likely to
// break silently: that a SIGNAL-borne interruption can never be recorded with a
// reason borrowed from the operator's keyboard.
//
// WHY A SEPARATE UNIT ARM ALONGSIDE THE REAL-SIGNAL ONE
// (chat_signal_interrupt_record_test.go): the real-signal test proves the whole
// path end to end, and it only ever exercises ONE signal (SIGINT) arriving ONE
// way. What it cannot cover is the residue — a run that Interrupt() reaches
// without any carrier having published a name (a hand-built agent, a carrier
// that fails to set CloseSignal first), where the reason must fall back to a
// member of the closed set rather than to the ctrl-c default. That fallback is
// exactly the "lie" this epic is about, so it is pinned here.

import (
	"os"
	"testing"

	"github.com/nick/gopi/internal/sessionevent"
)

// TestInterruptSourceVocabularyIsClosedAndDistinct pins the source vocabulary:
// both members exist, they are distinct, and the predicate agrees with the set.
func TestInterruptSourceVocabularyIsClosedAndDistinct(t *testing.T) {
	if len(InterruptSourceSet) < 2 {
		t.Fatalf("InterruptSourceSet has %d member(s) — the operator and the signal are the two ways a turn ends, and a set that cannot tell them apart cannot be closed", len(InterruptSourceSet))
	}
	seen := map[string]bool{}
	for _, s := range InterruptSourceSet {
		if s == "" {
			t.Error("InterruptSourceSet carries an empty member — an empty provenance string is what the field exists to prevent")
		}
		if seen[s] {
			t.Errorf("InterruptSourceSet lists %q twice", s)
		}
		seen[s] = true
		if !InterruptSourceKnown(s) {
			t.Errorf("InterruptSourceKnown(%q) is false for a member of its own set — the predicate and the set disagree", s)
		}
	}
	if InterruptSourceOperator == InterruptSourceSignal {
		t.Fatalf("the operator and the signal share the spelling %q — a reader could not tell who ended the turn", InterruptSourceOperator)
	}
	if InterruptSourceKnown("") || InterruptSourceKnown("human") || InterruptSourceKnown("SIGINT") {
		t.Error("InterruptSourceKnown accepts a value that is not in the set — the vocabulary is not closed")
	}
}

// TestSignalReasonNeverClaimsCtrlC is the epic's core assertion, at the unit
// level: with a signal's name published, the reason IS that name; with none
// published, the reason is the honest floor (InterruptReasonSignal) and never
// the operator's ctrl-c.
func TestSignalReasonNeverClaimsCtrlC(t *testing.T) {
	a := &Agent{}
	if got := a.SignalReason(); got != InterruptReasonSignal {
		t.Errorf("SignalReason() = %q with no CloseSignal, want %q — the fallback must not be a keypress nobody made", got, InterruptReasonSignal)
	}
	if a.SignalReason() == InterruptReasonCtrlC {
		t.Fatalf("SignalReason() answers %q for a run that recorded no signal — that is the false report this epic exists to remove", InterruptReasonCtrlC)
	}
	if !InterruptReasonKnown(a.SignalReason()) {
		t.Errorf("the signal fallback %q is not in InterruptReasonSet — a reader grouping by reason would see a value nothing defines", a.SignalReason())
	}
	// The name is CONSUMED, never derived: whatever the carrier published is what
	// the record carries, byte for byte (its spelling is signalrec's).
	a.CloseSignal = "SIGTERM"
	if got := a.SignalReason(); got != "SIGTERM" {
		t.Errorf("SignalReason() = %q after CloseSignal=SIGTERM, want the published name verbatim — this package must not re-spell a signal", got)
	}
	var nilAgent *Agent
	if got := nilAgent.SignalReason(); got != InterruptReasonSignal {
		t.Errorf("a nil agent's SignalReason() = %q, want the honest floor %q", got, InterruptReasonSignal)
	}
}

// TestRecordTurnInterruptionWritesTheSourceItWasGiven pins the writer's contract:
// the SOURCE argument reaches the JSONL unchanged, and the reason follows the
// source rather than always being TurnInterruptReason's answer.
//
// It is the unit-level counterpart of the real-signal arm: no process is spawned
// here, so a regression in the WRITER is caught even on a machine where the
// helper-process harness cannot run at all.
func TestRecordTurnInterruptionWritesTheSourceItWasGiven(t *testing.T) {
	for _, tc := range []struct {
		name   string
		source string
		// signalName is what a carrier published before Interrupt(); empty means
		// it published none.
		signalName string
		wantReason string
	}{
		{"operator", InterruptSourceOperator, "", InterruptReasonCtrlC},
		{"signal with a name", InterruptSourceSignal, "SIGINT", "SIGINT"},
		{"signal with no name", InterruptSourceSignal, "", InterruptReasonSignal},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sl, err := NewSessionLogger("deepseek-v4-pro", "")
			if err != nil {
				t.Fatalf("session logger: %v", err)
			}
			defer sl.Close()

			a := &Agent{SessionLog: sl, CloseSignal: tc.signalName}
			a.recordTurnInterruption(tc.source)

			raw, err := os.ReadFile(sl.Path())
			if err != nil {
				t.Fatalf("read the session log: %v", err)
			}
			ev := findTurnInterruptEvent(t, string(raw), sessionevent.EventTypeTurnInterrupted)
			if ev == nil {
				t.Fatalf("no %s witness after recordTurnInterruption(%q):\n%s", sessionevent.EventTypeTurnInterrupted, tc.source, string(raw))
			}
			if got, _ := ev["source"].(string); got != tc.source {
				t.Errorf("source = %q, want %q — the writer must record the source it was handed, not a hard-coded one", got, tc.source)
			}
			if got, _ := ev["reason"].(string); got != tc.wantReason {
				t.Errorf("reason = %q, want %q", got, tc.wantReason)
			}
			if tc.source == InterruptSourceSignal {
				if reason, _ := ev["reason"].(string); reason == InterruptReasonCtrlC {
					t.Errorf("a signal-ended turn recorded reason %q — no key was pressed", InterruptReasonCtrlC)
				}
			}
		})
	}
}

// TestRecordTurnInterruptionSurvivesAnUnknownSource: an invented source is SAID
// on stderr and then narrowed to the operator's — never written through. Silence
// here would put a value in a field scripts filter on that nothing defines
// (Rule 15).
func TestRecordTurnInterruptionSurvivesAnUnknownSource(t *testing.T) {
	sl, err := NewSessionLogger("deepseek-v4-pro", "")
	if err != nil {
		t.Fatalf("session logger: %v", err)
	}
	defer sl.Close()

	a := &Agent{SessionLog: sl}
	a.recordTurnInterruption("keyboard-goblin")

	raw, err := os.ReadFile(sl.Path())
	if err != nil {
		t.Fatalf("read the session log: %v", err)
	}
	ev := findTurnInterruptEvent(t, string(raw), sessionevent.EventTypeTurnInterrupted)
	if ev == nil {
		t.Fatalf("an unknown source must still produce a witness (narrowed, not dropped):\n%s", string(raw))
	}
	if got, _ := ev["source"].(string); !InterruptSourceKnown(got) {
		t.Errorf("source = %q is not in InterruptSourceSet %v — an invented provenance string reached the record", got, InterruptSourceSet)
	}
}

// TestInterruptReasonSetStillNamesTheKeys pins that adding the signal member did
// not disturb the operator vocabulary the carriers map onto: every key reason a
// carrier can produce is still a member, and the set stays closed.
func TestInterruptReasonSetStillNamesTheKeys(t *testing.T) {
	for _, r := range []string{InterruptReasonCtrlC, InterruptReasonEscape, InterruptReasonEOF, InterruptReasonShutdown, InterruptReasonClientGone, InterruptReasonSignal} {
		if !InterruptReasonKnown(r) {
			t.Errorf("%q is not in InterruptReasonSet %v", r, InterruptReasonSet)
		}
	}
	if InterruptReasonKnown("ctrl-C") || InterruptReasonKnown("SIGINT") {
		t.Error("InterruptReasonSet accepts a value nothing defines — the vocabulary is not closed (a signal's NAME is a reason the signal path records, but it is not a member of the KEY set)")
	}
}
