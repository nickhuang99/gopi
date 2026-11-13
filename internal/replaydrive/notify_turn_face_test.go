package replaydrive

// notify_turn_face_test.go — A RECORDED TURN KEEPS THE FACE IT ARRIVED UNDER, so
// the surface that re-types it can judge it with the SAME predicate that put it
// in Turns.
//
// THE MEASURED DEFECT this file guards (2026-11-04, session
// 0b51ae4a-fa82-41ba-8147-1ba3ebc92476). That session's screen reads
// `──── turn 1` … `──── turn 7`, one band per turn. Replaying it produced 7 bands
// numbered 1,2,2,2,2,2,2: five of the seven turns arrived from `gopi notify`, and
// a replay re-enters a recorded line as KEYSTROKES — so the seal stamped `human`,
// `sessionevent.IsTurnOpeningRow` denied the `[system] …` carrier text a turn, and
// the counter that advances only on a turn-opening row froze at 2.
//
// WHY THE FACE CANNOT BE RE-DERIVED FROM THE TEXT — the reason this file asserts a
// FIELD and not a string. The carrier text of a delivery and the carrier text of
// its RESTATEMENT are byte-identical, and the reader separates them by the row's
// recorded `source` alone (turninput.go leg 3). Text cannot recover it.
//
// WHAT IS ASSERTED, one arm per mistake:
//   - every turn CARRIES the face the loader judged it with (no turn is faceless);
//   - a `notify` row's turn carries `notify`, not the loader's `human` default;
//   - the face the turn carries IS the value `IsTurnOpeningRow` answers true for —
//     the same-source property, asserted rather than assumed (Rule 14);
//   - and the counter-argument: the same carrier text under the face a keystroke
//     produces (`human`) opens NO turn. That arm is what makes this test
//     red-on-the-old-tree rather than a tautology — it reproduces the freeze in
//     one call.

import (
	"testing"

	"github.com/nick/gopi/internal/inputsource"
	"github.com/nick/gopi/internal/sessionevent"
)

// notifyCarrier is the sentence `gopi notify --session … --message …` writes into
// the recording: the harness's own words, with the caller's text after them.
const notifyCarrier = "[system] An external process sent this session a message: task event: event=claimed task=01a19bf1-ff52"

// TestRecordedTurnCarriesTheFaceItArrivedUnder pins the field, per turn.
func TestRecordedTurnCarriesTheFaceItArrivedUnder(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		userRow("the operator's own line"),
		assistantRow(jsonTextBlock("first answer")),
		requestRow(),
		sourcedUserRow(notifyCarrier, inputsource.Notify),
		assistantRow(jsonTextBlock("answer to the delivery")),
		requestRow(),
		userRow("another typed line"),
		assistantRow(jsonTextBlock("second answer")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	if got, want := len(rec.Turns), 3; got != want {
		t.Fatalf("turns = %d, want %d — a delivery and two typed lines are three complete inputs", got, want)
	}
	want := []string{inputsource.Human, inputsource.Notify, inputsource.Human}
	for i, turn := range rec.Turns {
		if turn.Source == "" {
			t.Errorf("turn %d carries NO face — a replayed line would be typed under the arm's default, not the recording's", i)
			continue
		}
		if turn.Source != want[i] {
			t.Errorf("turn %d face = %q, want %q (text %q)", i, turn.Source, want[i], turn.Text)
		}
	}
}

// TestRecordedTurnFaceIsTheOneThePredicateJudged is the SAME-SOURCE arm: the
// face on the turn is not merely plausible, it is a value for which the ONE
// predicate says "this opens a turn". A turn in Turns that the predicate denies
// would mean the loader and the surface disagree about the same row — the class
// of defect this epic removes, one layer down.
func TestRecordedTurnFaceIsTheOneThePredicateJudged(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		sourcedUserRow(notifyCarrier, inputsource.Notify),
		assistantRow(jsonTextBlock("answer")),
		requestRow(),
		userRow("a typed line"),
		assistantRow(jsonTextBlock("answer")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	if len(rec.Turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(rec.Turns))
	}
	for i, turn := range rec.Turns {
		if !sessionevent.IsTurnOpeningRow(turn.Text, turn.Source) {
			t.Errorf("turn %d is in Turns but the predicate denies it a turn with its own face %q — the replay would print a band the counter does not advance",
				i, turn.Source)
		}
	}
}

// TestCarrierTextUnderAKeystrokeFaceOpensNoTurn is the OTHER direction, and it is
// what makes the two arms above non-vacuous: it reproduces the freeze in one call.
//
// A replay re-enters every line as a keystroke. The face a keystroke produces is
// `human`. Under THAT face the delivery's carrier text is a harness restatement —
// which the reader is right to refuse (leg 3: the same notice is re-injected at
// every round boundary, and counting those would multiply the turns). So the
// refusal is CORRECT, and the defect was never the predicate: it was that the
// replay handed the predicate a face the row never had.
func TestCarrierTextUnderAKeystrokeFaceOpensNoTurn(t *testing.T) {
	if sessionevent.IsTurnOpeningRow(notifyCarrier, inputsource.Human) {
		t.Fatalf("the carrier text under the human face opened a turn — the freeze this epic fixes would silently disappear, and the restatement leg (turninput.go rule 3) would be gone with it")
	}
	if !sessionevent.IsTurnOpeningRow(notifyCarrier, inputsource.Notify) {
		t.Fatalf("the carrier text under the notify face opened no turn — a delivery IS one complete input (the operator's 2026-10-27 ruling)")
	}
}

// TestTurnRowSourceDefaultsOnlyWhenTheRowRecordedNothing pins the ONE resolver's
// two answers, so a consumer cannot quietly reintroduce a second default.
func TestTurnRowSourceDefaultsOnlyWhenTheRowRecordedNothing(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"an unrecorded face is the person's", "", inputsource.Human},
		{"a recorded human face is returned verbatim", inputsource.Human, inputsource.Human},
		{"a recorded notify face is returned verbatim", inputsource.Notify, inputsource.Notify},
		{"a recorded queue face is returned verbatim", inputsource.Queue, inputsource.Queue},
		{"a recorded llm face is returned verbatim", inputsource.LLM, inputsource.LLM},
		// An unknown spelling is PASSED THROUGH, never mapped to `human`: a face
		// this binary cannot name is not one it should fabricate, and the
		// predicate then judges the row by its text instead of by an invented
		// provenance (Rule 15a).
		{"an unnameable face is not rewritten", "some-future-face", "some-future-face"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sessionevent.TurnRowSource(tc.in); got != tc.want {
				t.Errorf("TurnRowSource(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
