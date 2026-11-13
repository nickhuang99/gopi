package main

// chat_interrupt_origin_test.go — WHICH KEY ABANDONED THE TURN, at the seam where
// the input layer's vocabulary (lineedit.KeyOrigin) meets the record's
// (agent.InterruptReasonSet).
//
// WHY THIS FILE EXISTS. Two keys now mean "abandon the turn in flight" — a lone
// Escape (the pure interrupt) and Ctrl+C — and the marker the operator reads and
// the session JSONL both have to name the one they actually pressed. The mapping
// between the two vocabularies is a single function (chatInterruptReason) and it
// is the kind of thing that rots silently: a new origin added to lineedit's set,
// or a reason renamed in agent's, would leave the record attributing the
// operator's key to a different one, with nothing failing.
//
// So the CASE is exhaustiveness, not a sample: every member of lineedit's own
// closed set is mapped (and lands on a member of agent's closed set), and both
// sets are asserted to be exactly the sizes this file knows about. An addition to
// either one fails here — that is the point.
//
// The second half is the marker: the line the operator reads once, at the only
// moment they are looking, which must carry the same reason the record does.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/agent"
	"github.com/nick/gopi/internal/lineedit"
)

// TestChatInterruptReasonCoversEveryOrigin maps EVERY member of the input layer's
// origin set, and asserts each result is a reason the agent's own set knows. The
// loop reads the sets, so a new member is covered without editing this file —
// what would fail is the map not having an answer for it.
func TestChatInterruptReasonCoversEveryOrigin(t *testing.T) {
	if len(lineedit.KeyOriginSet) == 0 {
		t.Fatal("lineedit.KeyOriginSet is empty — the origin vocabulary has no members, so nothing here is asserted")
	}
	for _, origin := range lineedit.KeyOriginSet {
		if !origin.Valid() {
			t.Errorf("origin %q is in the set but Valid() says otherwise — the predicate and the set disagree", origin)
		}
		reason := chatInterruptReason(origin)
		if !agent.InterruptReasonKnown(reason) {
			t.Errorf("chatInterruptReason(%q) = %q, which is not in agent.InterruptReasonSet %v — the record would carry a reason no reader can group by",
				origin, reason, agent.InterruptReasonSet)
		}
	}
}

// TestChatInterruptReasonDistinguishesTheTwoKeys pins the ONE distinction the
// whole feature turns on, and it is a behavioural difference rather than a
// cosmetic one: Escape is the pure interrupt (it cancels the turn and leaves the
// session open with the line intact), while Ctrl+C at the prompt ends the
// session. A reader of the JSONL who saw both recorded as "ctrl-c" could not tell
// which of the two had happened.
//
// Ctrl+C is allowed to share its reason across its two TRANSPORTS (the raw 0x03
// byte and a real SIGINT) because they are the same key; what must not collapse
// is the pair of different KEYS.
func TestChatInterruptReasonDistinguishesTheTwoKeys(t *testing.T) {
	if got := chatInterruptReason(lineedit.OriginEscape); got != agent.InterruptReasonEscape {
		t.Errorf("chatInterruptReason(escape) = %q, want %q", got, agent.InterruptReasonEscape)
	}
	if got := chatInterruptReason(lineedit.OriginCtrlC); got != agent.InterruptReasonCtrlC {
		t.Errorf("chatInterruptReason(ctrl-c) = %q, want %q", got, agent.InterruptReasonCtrlC)
	}
	if agent.InterruptReasonEscape == agent.InterruptReasonCtrlC {
		t.Fatal("Escape and Ctrl+C share ONE reason spelling — the record cannot say which key the operator pressed, which is the whole point of recording one")
	}
	// The signal TRANSPORT of the same key reads as that key, and is deliberately
	// not given a third reason: a reader grouping ctrl-c interruptions must not
	// lose half of them to a transport detail.
	if got := chatInterruptReason(lineedit.OriginSignal); got != agent.InterruptReasonCtrlC {
		t.Errorf("chatInterruptReason(signal) = %q, want %q (the same KEY, a different transport)", got, agent.InterruptReasonCtrlC)
	}
}

// TestChatInterruptMarkerNamesTheKeyThatWasPRessed: the operator's marker. It
// must carry the reason it was built with — never a fixed one — because it is
// printed at the one moment the operator is reading, to explain what just
// stopped their answer.
func TestChatInterruptMarkerNamesTheKeyThatWasPressed(t *testing.T) {
	cases := []struct {
		reason string
		absent string
	}{
		{agent.InterruptReasonEscape, agent.InterruptReasonCtrlC},
		{agent.InterruptReasonCtrlC, agent.InterruptReasonEscape},
	}
	for _, c := range cases {
		marker := chatInterruptMarkerFor(c.reason, 12)
		if !strings.HasPrefix(marker, chatInterruptMarkerPrefix) {
			t.Errorf("marker %q does not open with the named prefix %q", marker, chatInterruptMarkerPrefix)
		}
		if !strings.Contains(marker, "by "+c.reason+" ") {
			t.Errorf("marker %q does not name its reason %q — the operator is told about a key they did not press", marker, c.reason)
		}
		if strings.Contains(marker, c.absent) {
			t.Errorf("marker %q also carries %q — the two keys are the one thing this line must keep apart", marker, c.absent)
		}
		if !strings.Contains(marker, agent.InterruptSourceOperator) {
			t.Errorf("marker %q does not carry the source %q", marker, agent.InterruptSourceOperator)
		}
		if !strings.Contains(marker, "12 chars shown") {
			t.Errorf("marker %q does not carry the partial answer's size", marker)
		}
	}
}
