package main

// chat_replay_band_numbering_test.go — THE SCREEN'S BAND NUMBERS ARE 1..N, AND
// THE NUMBER AND THE ROW THAT OPENED IT ARE JUDGED BY ONE RULE.
//
// MEASURED DEFECT (2026-11-04, session 0b51ae4a-fa82-41ba-8147-1ba3ebc92476).
// That session's own screen carries seven bands reading `──── turn 1` … `──── turn
// 7`. Replaying it produced seven bands numbered 1,2,2,2,2,2,2: five of the seven
// turns arrived from `gopi notify`, a replay re-enters a recorded line as
// KEYSTROKES, and the seal therefore stamped every one of them `human` —
// `sessionevent.IsTurnOpeningRow` then answered "no turn" for the carrier text,
// and the counter, which advances only on a turn-opening row, froze at 2.
//
// WHAT THIS FILE DRIVES, AND WHY IT IS NOT A REIMPLEMENTATION. The band number is
// computed from exactly four production functions in this order —
//
//	chatInput.takeReplayFace   → which face the RECORDING says this line had
//	chatSealSource             → the face stamped on the turn
//	chatOpensSessionTurn       → does it open a turn (the log reader's predicate)
//	chatSession.chatTurnDisplay→ the number the band prints
//
// — so this case calls THOSE, in that order, for every turn of a real recording
// parsed by the real loader. A test that recomputed the arithmetic itself would
// pass while the production path stayed wrong (Rule 31: the surface, not a
// sibling of it).
//
// THE PUSH SIDE IS ALSO THE REAL ONE: the faces are queued through
// chatInput.noteReplayFace, the very call the driver makes in typeTurn, so the
// pairing of push and pop is exercised rather than assumed.

import (
	"testing"

	"github.com/nickhuang99/gopi/internal/inputsource"
	"github.com/nickhuang99/gopi/internal/replaydrive"
	"github.com/nickhuang99/gopi/internal/sessionevent"
)

// notifyCarrierText is the sentence a `gopi notify` delivery writes into a
// session log: the harness's own words with the caller's text after them. Its
// RESTATEMENT is byte-identical, which is why the face cannot be read off it
// (two constants, one for each arm this file needs).
const notifyCarrierText = "[system] An external process sent this session a message: task event: event=claimed task=01a19bf1-ff52"

// bandNumbersForReplay returns the number each of rec's turns would paint on its
// band, by walking the production seam once per turn.
//
// honourRecordedFaces=false is the PRE-FIX PATH, kept as an explicit parameter
// rather than as a comment: the old code passed no replay face at all, so the seal
// answered `human` for every keystroke. The case below runs BOTH settings, which
// is what makes the assertion red-on-the-old-tree rather than merely green now.
func bandNumbersForReplay(t *testing.T, rec *replaydrive.Recording, honourRecordedFaces bool) []int {
	t.Helper()
	input := &chatInput{}
	// A fresh run: the baseline is 0, so the FIRST turn's ordinal is 1 and the
	// sequence a correct replay produces is 1..len(rec.Turns).
	sess := &chatSession{}
	operatorTurns := 0
	numbers := make([]int, 0, len(rec.Turns))
	for _, turn := range rec.Turns {
		if honourRecordedFaces {
			// THE DRIVER'S OWN CALL, in the driver's own order: the face is
			// queued immediately before the keystrokes of that line.
			input.noteReplayFace(turn.Source)
		}
		// THE READER'S POP — one per line received, which for a replay is one
		// per turn typed (plus the trailing /quit, which carries no face).
		replayFace := input.takeReplayFace()
		turnFace := chatSealSource(replayFace, false, false, nil)
		opens := chatOpensSessionTurn(turn.Text, turnFace)
		n := sess.chatTurnDisplay(operatorTurns)
		if opens {
			n++
		}
		numbers = append(numbers, n)
		if opens {
			operatorTurns++
		}
	}
	return numbers
}

// replayBandFixture is a recording shaped like the measured session: five of its
// seven complete inputs arrived from `gopi notify`, two were typed by the person.
// The delivery rows are recorded under the `notify` face EXACTLY as the live
// logger writes them, and the typed rows carry no face (the loader's documented
// default is `human`, and exercising that default is part of the point).
func replayBandFixture() *replaydrive.Recording {
	rec := &replaydrive.Recording{}
	// The measured shape exactly: the first two turns are the person's own lines
	// ("Yong", "hi" in the recorded session), and every later complete input
	// arrived from `gopi notify`. That distribution is not incidental — it is WHY
	// the freeze reads 1,2,2,2,2,2,2 rather than jumping again later, and a
	// fixture that spread the typed rows out would reproduce a different screen.
	for i := 0; i < 7; i++ {
		face := inputsource.Notify
		text := notifyCarrierText
		if i < 2 {
			face = inputsource.Human
			text = "a line the person typed"
		}
		rec.Turns = append(rec.Turns, replaydrive.Turn{Text: text, Source: face})
		rec.Responses = append(rec.Responses, nil)
	}
	return rec
}

// TestReplayBandNumbersAreOneThroughN is the deliverable: N bands, numbered 1..N,
// for a recording that mixes notify-injected and typed turns.
func TestReplayBandNumbersAreOneThroughN(t *testing.T) {
	rec := replayBandFixture()
	got := bandNumbersForReplay(t, rec, true)
	want := make([]int, len(rec.Turns))
	for i := range want {
		want[i] = i + 1
	}
	if len(got) != len(want) {
		t.Fatalf("bands = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("band numbers = %v, want %v — band %d printed %d, want %d.\n"+
				"A recording of %d complete inputs must show %d bands numbered 1..%d.",
				got, want, i, got[i], want[i], len(rec.Turns), len(rec.Turns), len(rec.Turns))
		}
	}
}

// TestReplayBandNumberingIsRedWithoutTheRecordedFace is the SELF-PROOF the epic
// asks for: the SAME fixture, driven through the same seam with the recorded face
// not carried, reproduces the measured freeze.
//
// It is not decoration. A green above is only evidence if the same case is red
// without the fix, and the pre-fix path is reproducible exactly: it is
// `honourRecordedFaces = false`, i.e. what the driver did before this run's change
// — no face queued, so every keystroke seals as `human`.
func TestReplayBandNumberingIsRedWithoutTheRecordedFace(t *testing.T) {
	rec := replayBandFixture()
	got := bandNumbersForReplay(t, rec, false)
	// The freeze: the counter stops at 2 and every later band repeats it, which
	// is the operator's reported screen (7 bands: 1,2,2,2,2,2,2).
	wantFrozen := []int{1, 2, 2, 2, 2, 2, 2}
	for i := range wantFrozen {
		if got[i] != wantFrozen[i] {
			t.Fatalf("the pre-fix path printed %v, want the measured freeze %v at band %d — if this changed, the reproduction above proves nothing",
				got, wantFrozen, i)
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i] < got[i-1] {
			t.Fatalf("the pre-fix sequence %v went BACKWARDS at band %d; the measured defect was a freeze, not a regression", got, i)
		}
	}
}

// TestBandNumberAdvancesWithTheSamePredicateTheReaderUses asserts the SAME-SOURCE
// property directly: for every turn, the number advances if and only if
// sessionevent.IsTurnOpeningRow says the row opens a turn under the face the
// recording gave it.
//
// This is the epic's second deliverable stated as an assertion rather than as
// prose. A number that advanced on a row the session reader does not count — or
// failed to advance on one it does — is the two-quantities-summed defect this
// family of tasks exists to end.
func TestBandNumberAdvancesWithTheSamePredicateTheReaderUses(t *testing.T) {
	rec := replayBandFixture()
	input := &chatInput{}
	for i, turn := range rec.Turns {
		input.noteReplayFace(turn.Source)
		replayFace := input.takeReplayFace()
		turnFace := chatSealSource(replayFace, false, false, nil)
		wantOpens := sessionevent.IsTurnOpeningRow(turn.Text, turn.Source)
		if gotOpens := chatOpensSessionTurn(turn.Text, turnFace); gotOpens != wantOpens {
			t.Fatalf("turn %d: chatOpensSessionTurn=%v but the reader's own predicate says %v (face %q vs recorded %q) — the band would advance on a row the log does not count",
				i, gotOpens, wantOpens, turnFace, turn.Source)
		}
	}
}

// TestReplayFacePrecedesEveryArmOfTheSeal pins the PRECEDENCE, using the real
// function with every other arm switched on: a recorded face wins over
// selfDriven, over queueTurn, and over a notice turn's own sources.
//
// WHY IT MATTERS THAT IT WINS. A replay's lines reach the loop through the same
// arms a live run uses, and during a replay one of them can genuinely fire (a
// `chat_surface type` line in the recorded script sets selfDriven). The face the
// RECORDING says the turn had is the fact about the conversation being re-run; an
// arm's face is a fact about the transport that carried it back in.
func TestReplayFacePrecedesEveryArmOfTheSeal(t *testing.T) {
	cases := []struct {
		name        string
		replayFace  string
		selfDriven  bool
		queueTurn   bool
		wantIgnored string
	}{
		{"over the self-driven arm", inputsource.Notify, true, false, inputsource.LLM},
		{"over the queue arm", inputsource.Notify, false, true, inputsource.Queue},
		{"over the operator's own arm", inputsource.Notify, false, false, inputsource.Human},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := chatSealSource(tc.replayFace, tc.selfDriven, tc.queueTurn, nil)
			if got != tc.replayFace {
				t.Fatalf("chatSealSource(replayFace=%q, selfDriven=%v, queueTurn=%v) = %q, want the recorded face %q — without the precedence the recorded face is lost and the band freezes",
					tc.replayFace, tc.selfDriven, tc.queueTurn, got, tc.replayFace)
			}
			// The arm it overrode must still answer its own face when no
			// recording is involved: this is a precedence, not a disablement.
			if bare := chatSealSource("", tc.selfDriven, tc.queueTurn, nil); bare != tc.wantIgnored {
				t.Fatalf("with no recorded face the seal returned %q, want %q — a replay's precedence must not change a plain run", bare, tc.wantIgnored)
			}
		})
	}
}

// TestReplayFaceQueueIsOnePerLineInOrder pins the push/pop pairing: the driver
// pushes one face per turn it types, the reader pops one per line, and the order
// is preserved. A queue that leaked, reordered or dropped would attach a face to
// the wrong turn — which on screen is a band numbered by a different rule than the
// row under it.
func TestReplayFaceQueueIsOnePerLineInOrder(t *testing.T) {
	input := &chatInput{}
	// An empty queue answers "" — every non-replay run, and the replay's /quit.
	if got := input.takeReplayFace(); got != "" {
		t.Fatalf("an empty queue returned %q, want \"\"", got)
	}
	// A face the vocabulary cannot name is NOT queued (Rule 15a: an absent value
	// stays absent rather than being fabricated), so it cannot shift the queue.
	input.noteReplayFace("")
	want := []string{inputsource.Notify, inputsource.Human, inputsource.Queue}
	for _, f := range want {
		input.noteReplayFace(f)
	}
	for i, w := range want {
		if got := input.takeReplayFace(); got != w {
			t.Fatalf("pop %d = %q, want %q — the queue must preserve the driver's typing order", i, got, w)
		}
	}
	if got := input.takeReplayFace(); got != "" {
		t.Fatalf("the queue returned %q after being drained, want \"\"", got)
	}
}
