package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/sessionevent"
)

// TestChatCountsAsOperatorTurn pins WHICH FACES open a turn the session log
// counts. It is the unit gate the status row was missing (task 01a17492).
//
// THE TABLE WAS REWRITTEN BY THE 2026-10-27 RULING, and the old values are named
// here so a later reader can see WHY they changed rather than assume the test was
// relaxed. It used to assert Notify=false and LLM=false, on the reasoning that
// both are spelled "[system] …" and the reader's predicate was "not a harness
// marker". That predicate is gone: the reader decides by the row's recorded FACE
// (sessionevent.IsTurnOpeningRow), under which a `gopi notify` delivery and a
// `chat_surface action=type` self-typed line are both COMPLETE INPUTS — the old
// assertion was pinned to the defect, not to the rule.
//
// The property that must hold is the one this table cannot show by itself and the
// session reader now guarantees: the log's turn count and this display's advance
// move by one for the SAME rows.
func TestChatCountsAsOperatorTurn(t *testing.T) {
	cases := []struct {
		source string
		want   bool
	}{
		// The operator at the prompt.
		{inputsource.Human, true},
		// A prompt the operator PARKED: same author, different producer.
		{inputsource.Queue, true},
		// Another process delivering into the inbox: one complete input, and the
		// reader opens a turn at it (sessionevent.IsTurnOpeningRow's first leg).
		// WAS false before the 2026-10-27 ruling.
		{inputsource.Notify, true},
		// The model typing its own next line (`chat_surface action=type`): also a
		// complete input, and the reader counts it. WAS false.
		{inputsource.LLM, true},
		// An unnameable face is not silently promoted to a counted one.
		{"", false},
		{"some-unknown-face", false},
	}
	for _, c := range cases {
		if got := chatCountsAsOperatorTurn(c.source); got != c.want {
			t.Errorf("chatCountsAsOperatorTurn(%q) = %v, want %v", c.source, got, c.want)
		}
	}
}

// TestChatTurnDisplayAddsLikeUnits reproduces the DEFECT's own arithmetic, so a
// future change that re-folds the pass count into the display number fails here
// instead of on an operator's screen.
//
// The measured case (session 41e5dade, 2026-10-26):
//
//	turnBaseline (log turns already held) = 38   ← gopilog.SessionTurns
//	REPL passes this run  (iterCount)     = 82   ← counted notify-driven turns
//	operator rows this run                = 39   ← what the log will count
//
// The old code printed chatTurnDisplay(iterCount) = 38+82 = 120 — the number in
// the bug report. The two sides of that sum were different quantities: the
// baseline counted OPERATOR rows and iterCount counted EVERY pass. The correct
// number is 38+39 = 77, and it is correct for a reason the log can confirm: it
// is the count of rows the log will stamp as turns.
func TestChatTurnDisplayAddsLikeUnits(t *testing.T) {
	const (
		baseline      = 38
		passesThisRun = 82
		operatorRows  = 39
		notifyRows    = passesThisRun - operatorRows
	)
	if notifyRows <= 0 {
		t.Fatalf("the fixture no longer reproduces the defect: %d passes for %d operator rows", passesThisRun, operatorRows)
	}
	sess := &chatSession{turnBaseline: baseline}

	// The WRONG sum, still available and still wrong — asserted so nobody reads
	// this test as "the two numbers happen to agree".
	if got := sess.chatTurnDisplay(passesThisRun); got != 120 {
		t.Fatalf("chatTurnDisplay(passes) = %d, want the reported 120 — the fixture no longer models the defect", got)
	}

	// The RIGHT sum: the baseline and this run's operator rows.
	if got, want := sess.chatTurnDisplay(operatorRows), baseline+operatorRows; got != want {
		t.Fatalf("chatTurnDisplay(operator rows) = %d, want %d", got, want)
	}

	// THE DRIFT GUARD, RESTATED (operator's ruling, 2026-10-27). It used to
	// assert `!chatCountsAsOperatorTurn(Notify)`, on the reasoning that a notify
	// delivery "advances no operator row, which is why the pass count overtook
	// the turn count". THAT REASONING WAS WRONG: the pass count overtook the turn
	// count because a turn takes MANY passes — the 82-vs-39 gap above is passes,
	// not deliveries — and in the meantime the same assertion pinned the DISPLAY
	// to a predicate the LOG no longer uses, which is the drift it was written to
	// prevent, arriving from the other side.
	//
	// The property that actually prevents drift is AGREEMENT: whatever the log
	// counts as a turn, the display advances for. So the arm now walks every face
	// the REPL can stamp and requires the two predicates to say the same thing.
	for _, face := range []string{inputsource.Human, inputsource.Queue, inputsource.Notify, inputsource.LLM} {
		logged := chatFaceOpensSessionTurn(face)
		if !logged {
			t.Fatalf("face %q opens no SESSION turn — the REPL's display and `session log --turns` disagree the moment the REPL stamps it", face)
		}
		if got := chatCountsAsOperatorTurn(face); got != logged {
			t.Fatalf("face %q: display counts=%v but the log counts=%v — that IS the drift", face, got, logged)
		}
	}
	// And an unnameable face is counted by neither side: the display must not
	// invent a turn the log will not have.
	if chatCountsAsOperatorTurn("some-unknown-face") {
		t.Fatal("an unnameable face is counted by the display but not by the log — the display would drift above the log")
	}

	// A fresh session still numbers from 1: the fix must not turn "no baseline"
	// into an off-by-one.
	fresh := &chatSession{}
	if got := fresh.chatTurnDisplay(1); got != 1 {
		t.Fatalf("a fresh session's first turn = %d, want 1", got)
	}
}

// chatFaceOpensSessionTurn asks THE PREDICATE THE LOG USES, so the test below
// compares the display against the reader instead of against a second opinion
// about what the reader ought to think. The empty row text is the point: outside
// the delivery carrier, the FACE is what decides, not the words.
func chatFaceOpensSessionTurn(face string) bool {
	return sessionevent.IsTurnOpeningRow("", face)
}

// TestSessionPassCountIsNotTheRoundCount pins the second half of the same
// defect: a surface must never print the ROUND count under the PASS word.
//
// The reader used to fill TotalIterations with the `request`-row count whenever
// the session recorded no `session_end`, and three render sites printed it as
// "Passes". On session 41e5dade that produced "Iterations: 468 / Passes: 468"
// — one number under two words — while the live screen of the same run read
// turn120. A pass count has exactly ONE producer, so its absence is printed as
// an absence (sessionevent.CounterPassUnrecorded), never as a neighbour's
// measurement.
func TestSessionPassCountIsNotTheRoundCount(t *testing.T) {
	// The unit words themselves are the contract: the pass word must not be the
	// round word, and the absence must be stated rather than left blank.
	if sessionevent.CounterUnitLoopPass == sessionevent.CounterUnitRound {
		t.Fatalf("the pass unit and the round unit are the same word (%q) — one word, two quantities", sessionevent.CounterUnitLoopPass)
	}
	if sessionevent.CounterPassUnrecorded == "" {
		t.Fatal("CounterPassUnrecorded is empty: an unrecorded pass count would print as nothing at all")
	}
	if sessionevent.CounterPassUnrecorded == sessionevent.CounterSourceDerived {
		t.Fatal("an unrecorded pass count is described with the derived-figure caption — that is the substitution this fix removes")
	}
}

// TestChatTurnDisplaySemanticsAreWrittenDown pins WHICH number each surface
// prints, because the numbers are one apart and the word is the same (task
// 01a17a37, measured on session 4a0957a3-bb4e-4337-a761-20606c87d1fd: the same
// session was called 29 by `session show`, 29 by the resume notice's "already
// holds", and 30 by the same notice's "the next turn is turn 30").
//
// THE TWO POSITIONS, and they must never be confused:
//
//	THE STATUS BAR / INFO BAR = chatTurnDisplay(operatorTurns) = the COMPLETED
//	  count. At the idle prompt before this run's first turn it equals the
//	  session's baseline alone — the same number `gopi session show` prints.
//	THE BODY HEADING = the ORDINAL of the turn about to run = the completed
//	  count + 1 (the caller adds it when the turn opens a session turn), which is
//	  the number the resume notice promises as "the next turn".
//
// A future change that prints iterCount here, or that prints the ordinal on the
// bar, fails below — and the failure names which of the two it broke.
func TestChatTurnDisplaySemanticsAreWrittenDown(t *testing.T) {
	const baseline, turnsThisRun = 29, 3
	sess := &chatSession{turnBaseline: baseline}

	// The bar, before any turn of this run: the session's WHOLE history, i.e.
	// exactly what `gopi session show <id>` prints under `turns`.
	if got, want := sess.chatTurnDisplay(0), baseline; got != want {
		t.Fatalf("the idle bar = %d, want the session's completed count %d (the number `session show` prints)", got, want)
	}
	// After this run's turns: still a COMPLETED count, advanced by them.
	if got, want := sess.chatTurnDisplay(turnsThisRun), baseline+turnsThisRun; got != want {
		t.Fatalf("the bar after %d turns = %d, want %d", turnsThisRun, got, want)
	}
	// The heading is that count plus one: the ORDINAL of the turn about to run.
	// Both numbers come from chatTurnDisplay, so neither can drift from it.
	if got, want := sess.chatTurnDisplay(turnsThisRun)+1, baseline+turnsThisRun+1; got != want {
		t.Fatalf("the heading ordinal = %d, want %d", got, want)
	}
	// THE RESUME NOTICE names the same two numbers (chatResumeTurnBaselineFormat
	// is fed baseline, baseline+1): if the bar ever stopped agreeing with it, the
	// operator would read two answers one screen apart — the defect this pins.
	if got := fmt.Sprintf(chatResumeTurnBaselineFormat, baseline, baseline+1); !strings.Contains(got, " 29 turn(s) ") || !strings.Contains(got, "turn 30") {
		t.Fatalf("the resume notice no longer names the completed count and the ordinal in the numbers this bar derives:\n%s", got)
	}
}

// TestChatOpensSessionTurnIsTheReadersQuestion pins the REPL's counter to the
// session READER's counter, on the rows they actually disagreed about.
//
// THE MEASURED DISAGREEMENT (2026-10-27, session
// 80c07a7d-bb6b-48b4-be66-ea1867f403f2): `chats.turn_count` held 18 while
// `gopilog.SessionTurns` over the same log counted 11. The seven-row gap was
// harness chatter written from the human FACE — a row whose text is the framework
// talking, which the reader opens no turn at. The REPL decided with the face
// alone (chatCountsAsOperatorTurn), so its counter advanced past the number
// `session log --turns` prints, and the ledger followed it.
//
// WHAT IS ASSERTED, and it is deliberately one property rather than a table of my
// expectations: for EVERY face the recorder can stamp, the REPL charges a turn iff
// the READER opens one. The notify/llm cases are true on both sides by the
// operator's 2026-10-27 ruling (a delivered notify is one complete input); the
// chatter row is where the two used to disagree, and it is the case that fails if
// this helper ever goes back to deciding by face.
func TestChatOpensSessionTurnIsTheReadersQuestion(t *testing.T) {
	chatter := "[system] Background job done: build (exit 0) — no output"
	typed := "把我刚才那个 commit 推到 master 上"
	faces := []string{inputsource.Human, inputsource.Queue, inputsource.Notify, inputsource.LLM}

	for _, face := range faces {
		for _, msg := range []string{typed, chatter} {
			got := chatOpensSessionTurn(msg, face)
			want := sessionevent.IsTurnOpeningRow(msg, face)
			if got != want {
				t.Fatalf("chatOpensSessionTurn(%.40q, %q) = %v but the READER says %v — the REPL's counter and `session log --turns` describe the same session and must not diverge",
					msg, face, got, want)
			}
		}
	}
	// The row the ledger used to charge and the log never opened — named on its
	// own, because a table-driven agreement cannot show WHICH row was the defect.
	if chatOpensSessionTurn(chatter, inputsource.Human) {
		t.Fatalf("a harness-chatter row on the human face charged a turn: this is exactly the row that made the ledger 18 against the reader's 11")
	}
	// And the ordinary case still counts, so a fix that charged nothing at all
	// would fail here.
	if !chatOpensSessionTurn(typed, inputsource.Human) {
		t.Fatalf("a row the person typed charged no turn")
	}
}
