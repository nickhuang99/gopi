package replaydrive

// turn_opened_at_test.go — A RECORDED TURN KEEPS THE MOMENT ITS ROW WAS WRITTEN,
// so the surface that re-types it can state WHEN that turn opened instead of the
// moment somebody happened to replay it.
//
// THE MEASURED DEFECT this file guards (2026-11-08). Session 6b01dd46 opened at
// 2026-11-07 12:11:17. Replaying it painted
//
//	turn 1 · 2026-11-08 15:59:32 · 现在是，嗯，这样的…
//
// — TODAY, on a recording a day old — because a replay re-enters a recorded turn
// as KEYSTROKES and the REPL mints a turn's moment from its own clock
// (cmd/agent/cli_chat.go), which is the truth for a LIVE run and a lie for a
// recorded one. The recording already holds the moment: the `timestamp` of the
// row that OPENED the turn.
//
// WHY THE MOMENT CANNOT BE RE-DERIVED LATER — the reason this file asserts a
// FIELD and not an arithmetic: the row is gone by the time the banner paints. The
// loader is the only place that still holds it, so a turn that reaches the
// surface without it has lost it for good, and the surface's only remaining
// options are the clock (wrong) or a placeholder (honest).
//
// WHAT IS ASSERTED, one arm per mistake:
//   - every turn CARRIES the moment its row recorded (no turn is momentless when
//     the record had one), in the RECORDING's own zone — a turn written at
//     12:11:17 +08:00 must not print as the same instant restated elsewhere;
//   - a row with NO timestamp yields the ZERO value, which is what lets the
//     banner state the shared placeholder — the shape that must never be filled
//     in with time.Now();
//   - a row whose timestamp is UNREADABLE is the same absence, not an error: a
//     truncated or hand-edited record must still replay;
//   - and the moments are PER TURN, in order — an implementation that read only
//     the file's first row would pass an "is it set" check and print one moment
//     on every band.

import (
	"encoding/json"
	"testing"
	"time"
)

// stampedUserRow is userRow spelled with the row's own write timestamp, which is
// the shape a live session log writes (the field is the row's, not the turn's).
func stampedUserRow(text, stamp string) string {
	b, _ := json.Marshal(text) //stylegate:ignore marshalling a string cannot fail
	return `{"type":"user","timestamp":"` + stamp + `","message":{"role":"user","content":` + string(b) + `}}`
}

// TestRecordedTurnCarriesTheMomentItsRowWasWritten pins the field, per turn, and
// pins the ZONE: the banner must print the wall clock the session ran on.
func TestRecordedTurnCarriesTheMomentItsRowWasWritten(t *testing.T) {
	const (
		first  = "2026-11-07T12:11:17.247376654+08:00"
		second = "2026-11-08T09:03:41.000000001+08:00"
	)
	p := writeJSONL(t, "session.jsonl", []string{
		stampedUserRow("the operator's first line", first),
		assistantRow(jsonTextBlock("first answer")),
		requestRow(),
		stampedUserRow("the operator's second line", second),
		assistantRow(jsonTextBlock("second answer")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	if got, want := len(rec.Turns), 2; got != want {
		t.Fatalf("turns = %d, want %d", got, want)
	}
	for i, want := range []string{first, second} {
		wantAt, err := time.Parse(time.RFC3339, want)
		if err != nil {
			t.Fatalf("fixture stamp %q does not parse: %v", want, err)
		}
		got := rec.Turns[i].OpenedAt
		if got.IsZero() {
			t.Errorf("turn %d carries NO moment — a replay would fall through to the clock and print today", i)
			continue
		}
		if !got.Equal(wantAt) {
			t.Errorf("turn %d moment = %s, want %s", i, got.Format(time.RFC3339), wantAt.Format(time.RFC3339))
		}
		// THE ZONE IS THE RECORD'S, not the reader's: a replayed banner states
		// the hour the session ran, and a value normalised to UTC would print a
		// different wall clock for the very same instant.
		if name, off := got.Zone(); off != 8*3600 {
			t.Errorf("turn %d moment is in zone %q (offset %d), want the recording's +08:00 — the banner would state a different wall clock", i, name, off)
		}
	}
}

// TestRecordedTurnWithoutATimestampCarriesTheZeroMoment is the ABSENCE arm, and
// it is the one that keeps the banner honest: a zero moment is what the surface
// reads as "state the placeholder". A loader that skipped the field, or that
// substituted the file's first row, would make this turn claim a time it does not
// have.
func TestRecordedTurnWithoutATimestampCarriesTheZeroMoment(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		// NO timestamp on the turn-opening row at all: a record written by an
		// older writer, or hand-edited.
		userRow("a line whose row recorded no moment"),
		assistantRow(jsonTextBlock("still answered")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording: %v", err)
	}
	if len(rec.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(rec.Turns))
	}
	if got := rec.Turns[0].OpenedAt; !got.IsZero() {
		t.Fatalf("a row with no timestamp yielded %s — the banner would state a moment the record never held, and the placeholder could never be reached",
			got.Format(time.RFC3339))
	}
}

// TestRecordedTurnWithAnUnreadableTimestampIsAnAbsenceNotARefusal is the
// TRUNCATION arm: a half-written stamp is a record this replay must still be able
// to re-run, and the honest answer is the placeholder — never a parse error that
// takes the whole recording down, and never a guess.
func TestRecordedTurnWithAnUnreadableTimestampIsAnAbsenceNotARefusal(t *testing.T) {
	p := writeJSONL(t, "session.jsonl", []string{
		stampedUserRow("a line whose stamp was truncated", "2026-11-0"),
		assistantRow(jsonTextBlock("answered anyway")),
		requestRow(),
	})
	rec, err := LoadRecording(p)
	if err != nil {
		t.Fatalf("LoadRecording refused a recording over one unreadable stamp: %v", err)
	}
	if len(rec.Turns) != 1 {
		t.Fatalf("turns = %d, want 1", len(rec.Turns))
	}
	if got := rec.Turns[0].OpenedAt; !got.IsZero() {
		t.Fatalf("a truncated stamp yielded %s — a guess dressed as a record", got.Format(time.RFC3339))
	}
}
