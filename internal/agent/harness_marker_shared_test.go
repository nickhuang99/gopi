package agent

// harness_marker_shared_test.go — the CROSS-SIDE guard for the defect measured
// on session 80c07a7d (2026-10-25): three surfaces reported three turn counts
// (18 / 17 / 23) for one recorded conversation, because the harness WROTE its
// injections from four markers while the leaf predicate the readers and the
// replay share recognised one of them.
//
// The fix moves every marker into internal/sessionevent and makes this
// package's own names ALIASES of them. Two things therefore have to stay true,
// and each is asserted here:
//
//  1. the anchor policy's slice (harnessMessageMarkers) IS the leaf's slice —
//     not a copy that happens to agree today (Rule 14: one definition), and
//  2. the two predicates this repository has for "whose row is this?" — this
//     package's isHarnessInjectedUserMessage and the leaf's
//     sessionevent.IsHarnessInjected — return the SAME answer for every marker
//     and for operator text.
//
// Arm 2 is the one that would have caught 80c07a7d: it fails the moment either
// side gains a marker the other cannot see, which is exactly how 12
// `[SELF-RECORD …]` re-injections came to be counted as the person's own asks.

import (
	"strings"
	"testing"

	"github.com/nick/gopi/internal/deepseek"
	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/sessionevent"
)

// TestTheMarkerSetIsTheLeafsOne pins Rule 14 from the writing side: the slice
// the anchor policy and the compaction-floor predicate read is the SAME value
// the readers consult, not a second spelling of it.
func TestTheMarkerSetIsTheLeafsOne(t *testing.T) {
	if len(harnessMessageMarkers) != len(sessionevent.HarnessMessageMarkers) {
		t.Fatalf("harnessMessageMarkers has %d entries, sessionevent.HarnessMessageMarkers has %d — one of the two lists drifted, and a marker only one side sees is a row the other side misattributes",
			len(harnessMessageMarkers), len(sessionevent.HarnessMessageMarkers))
	}
	for i := range harnessMessageMarkers {
		if harnessMessageMarkers[i] != sessionevent.HarnessMessageMarkers[i] {
			t.Errorf("marker %d: agent = %q, sessionevent = %q", i, harnessMessageMarkers[i], sessionevent.HarnessMessageMarkers[i])
		}
	}
	// The named aliases must still BE the leaf's values: the append sites in
	// this package read them, so a re-spelling here would put a marker back into
	// the place the fix moved it out of.
	aliases := map[string]string{
		"consultCheckpointMarker":   sessionevent.HarnessConsultCheckpointMarker,
		"taskAreaConclusionsMarker": sessionevent.HarnessTaskAreaConclusionsMarker,
		"selfRecordMarker":          sessionevent.HarnessSelfRecordMarker,
		"harnessMessagePrefix":      sessionevent.HarnessMessagePrefix,
	}
	for name, want := range aliases {
		var got string
		switch name {
		case "consultCheckpointMarker":
			got = consultCheckpointMarker
		case "taskAreaConclusionsMarker":
			got = taskAreaConclusionsMarker
		case "selfRecordMarker":
			got = selfRecordMarker
		case "harnessMessagePrefix":
			got = harnessMessagePrefix
		}
		if got != want {
			t.Errorf("%s = %q, want the leaf's %q (Rule 14: an alias, never a second definition)", name, got, want)
		}
	}
}

// TestBothSidesAgreeOnEveryMarker is the agreement arm: for every marker the
// harness can inject, and for operator text, the two predicates answer alike.
//
// `[SELF-RECORD …]` is stated as its own case because it is the row that
// produced the epic: it is the run's OWN textual record, which is what made it
// look like something the person typed, and it is still an INJECTION — no
// person typed it and the recorded screen never showed it as their line.
func TestBothSidesAgreeOnEveryMarker(t *testing.T) {
	carrier := selfRecordMarker + "\nThese are the notes YOU recorded in this session's earlier rounds."
	if isHarnessInjectedUserMessage(deepseek.Message{Role: "user", Content: carrier}) != true {
		t.Fatalf("the [SELF-RECORD …] carrier is NOT a harness injection to this package — it will be taken for the operator's own ask")
	}
	// The specific regression: before the fix the leaf said the carrier was a
	// human turn while this package said it was an injection, so the replay
	// typed it (23) and the log counted its gaps (17) against 11 real rows.
	if sessionevent.IsTurnOpeningRow(carrier, "") {
		t.Fatalf("the [SELF-RECORD …] carrier OPENS A TURN — the replay will type the harness's own note block as if the person had")
	}
	// AND THE ONE ROW THE OLD COMPLEMENT COULD NOT DESCRIBE: a `gopi notify`
	// delivery is harness-written text (the sentence above) that IS a complete
	// input, so it opens a turn even though this package calls it an injection.
	// A tree that reinstates `IsTurnOpeningRow == !IsHarnessInjected` fails here.
	delivery := harnessMessagePrefix + sessionevent.HarnessInboxNoticeWord + "task event: event=failed"
	if !isHarnessInjectedUserMessage(deepseek.Message{Role: "user", Content: delivery}) {
		t.Fatalf("the delivery carrier is NOT a harness injection to this package — the arm below would prove nothing")
	}
	if !sessionevent.IsTurnOpeningRow(delivery, inputsource.Notify) {
		t.Fatalf("a `gopi notify` DELIVERY does not open a turn — the operator's ruling (2026-10-27) that it is one complete input is not implemented")
	}
	cases := []struct {
		name string
		text string
		// opensTurn is what IsTurnOpeningRow must answer for THIS row, written
		// from the HUMAN face — the face every row in this table carries. It is
		// stated PER ROW and never derived from harness-injected, because the two
		// predicates answer two DIFFERENT questions (see the assertion below):
		// machinery opens nothing, while a briefing and the person's own line do.
		opensTurn bool
	}{
		{"the prefix", harnessMessagePrefix + "[t+~11h] — +1s since your previous request.", false},
		{"the prefix after leading whitespace", "\n\n" + harnessMessagePrefix + "TASK-START CHECKPOINT", false},
		{"the memory-consult checkpoint", consultCheckpointMarker + " This round: which work could a memory search have saved?", false},
		{"the committed-conclusions block", taskAreaConclusionsMarker + "\n- a conclusion", false},
		{"the self-record carrier", carrier, false},
		// THE STDIN BRIEFING MARKER, and it needs its own row because it is the
		// one marker whose row is NOT written by this package's loop: a PARENT
		// process hands the text over on stdin and the child's record files it
		// `source=human` (row 01a188da). If either predicate stopped recognising
		// it, the framework's instructions would be read as the operator's
		// opening turn on every dispatched run.
		{"a marked stdin briefing", sessionevent.BriefingOnStdin("[EPIC GOAL]\n[Task summary]\nbug: x"), true},
		{"a marked briefing without a frame", sessionevent.BriefingOnStdin("You are an independent verification instance …"), true},
		// The composition the DISPATCHED child actually records (measured on this
		// row's own run): the frame around the marked text.
		{"the framed+marked row", sessionevent.HarnessEpicGoalFrameMarker + "\n" + sessionevent.BriefingOnStdin("[Task summary]\nbug: x"), true},
		// THE COUNTER-EXAMPLE THIS DESIGN DELIBERATELY KEEPS: the BARE frame around
		// an unmarked body is NOT an injection (this profile's tree holds 1118 such
		// rows; calling them injections reclassifies the corpus).
		{"the bare frame around an unmarked body", sessionevent.HarnessEpicGoalFrameMarker + "\n[Task summary]\nbug: x", true},
		{"an operator ask", "你自己去执行去看看", true},
		{"an operator ask mentioning a marker", "what does [SELF-RECORD — your own notes, survived compaction] do?", true},
		{"the empty row", "", true},
	}
	// The two predicates answer two DIFFERENT questions, and this arm proved it
	// the hard way: until 2026-11-01 it asserted they were COMPLEMENTS on the
	// human face (`want == IsTurnOpeningRow(...)` is an error), which held only
	// while "framework-written" and "opens no turn" were the same fact.
	//
	// THE BRIEFING LEG BROKE THAT ON THIS VERY FACE. A dispatched run has no
	// operator, so the briefing its parent handed it on stdin is the ONE complete
	// input the session ever receives — and the child's record files that row
	// `source=human`, because the stdin seam has no other honest face (commit
	// 5ac72c0c, row 01a188da). So the row is harness-injected AND opens a turn,
	// under exactly the face this table carries. e913b2a0 added that leg to
	// IsTurnOpeningRow and 17f2e8a8 flipped sessionevent's own arms to match; the
	// complement assertion here was left standing and has been failing ever since
	// on the three briefing rows — which is the whole of the measured failure, not
	// a stale baseline (HEAD 64881aef, 2026-11-01).
	//
	// What is pinned instead is the relation that actually holds, and it is
	// STRONGER than the retired one: every row's turn answer is stated per row
	// above (`opensTurn`), the text-provenance answer still comes from the leaf
	// predicate, and BOTH classes must be present in this table — a harness-
	// injected row that opens no turn (machinery: the clock tick, the checkpoints,
	// the self-record carrier) and a harness-injected row that DOES open a turn
	// (the briefing, the other half of the same fact). A tree that reinstates the
	// complement cannot satisfy both counters at once; a tree that loses the
	// briefing leg loses the second.
	var injectedClosing, injectedOpening int
	for _, c := range cases {
		want := sessionevent.IsHarnessInjected(c.text)
		got := isHarnessInjectedUserMessage(deepseek.Message{Role: "user", Content: c.text})
		if got != want {
			t.Errorf("%s: this package says harness=%v, sessionevent says %v — one recorded row, two answers", c.name, got, want)
		}
		if opens := sessionevent.IsTurnOpeningRow(c.text, ""); opens != c.opensTurn {
			t.Errorf("%s: IsTurnOpeningRow on the human face = %v, want %v — a harness-injected row opens a turn iff it is a COMPLETE INPUT (the briefing is; a clock tick is not)", c.name, opens, c.opensTurn)
		}
		switch {
		case want && !c.opensTurn:
			injectedClosing++
		case want && c.opensTurn:
			injectedOpening++
		}
	}
	if injectedClosing == 0 {
		t.Error("no harness-injected row in this table opens NO turn — the machinery class (clock tick, checkpoints, self-record carrier) is unrepresented, so this arm no longer proves that injections are not inputs")
	}
	if injectedOpening == 0 {
		t.Error("no harness-injected row in this table OPENS a turn — the briefing class is unrepresented, and the retired complement could come back with this arm still green")
	}
}

// TestTheCarrierIsStillNotCompressedIntoMemory is the neighbour guard, so the
// classifier change cannot quietly cost the note block its exemption: the note
// carrier is a NON-compressible message (its record is the point), and it is now
// ALSO classified as the harness's row. The two facts are independent and this
// pins that the fix did not trade one for the other.
func TestTheCarrierIsStillNotCompressedIntoMemory(t *testing.T) {
	carrier := selfRecordMarker + "\nThese are the notes YOU recorded in this session's earlier rounds."
	if !isNonCompressibleMessage(deepseek.Message{Role: "user", Content: carrier}) {
		t.Fatal("the self-record carrier is no longer non-compressible — a compaction would fold the run's own record away")
	}
	if !strings.HasPrefix(carrier, selfRecordMarker) {
		t.Fatal("the carrier no longer OPENS with its marker, so the classifier above is testing a different shape")
	}
}
