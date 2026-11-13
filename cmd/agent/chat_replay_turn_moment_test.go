package main

// chat_replay_turn_moment_test.go — THE BAND STATES THE RECORDED MOMENT ON A
// REPLAY, and states it from the RECORD rather than from this process's clock.
//
// THE MEASURED DEFECT this file guards (2026-11-08). A replay re-enters a
// recorded turn as KEYSTROKES, and the REPL mints a turn's moment from
// time.Now() because on a LIVE run that is the truth. Replayed, it painted
//
//	turn 1 · 2026-11-08 15:59:32 · 现在是，嗯，这样的…
//
// for a session whose first turn opened 2026-11-07 12:11:17 — TODAY, on a
// recording a day old. The recording holds the moment, so the surface asks the
// record; the fix is the pairing this file walks.
//
// THE FIELD THAT CARRIES THE DISTINCTION IS `replayStamped`, NOT THE MOMENT'S
// ZERO-NESS, and that is what these arms exist to keep true: a recorded turn
// whose row held no timestamp must reach the band as an ABSENCE (so it prints the
// shared placeholder), while an ordinary LIVE line must keep reaching the band as
// a clock reading. Any implementation that keyed on `!moment.IsZero()` would make
// a momentless old record fall through to time.Now() — the very defect — and any
// implementation that keyed on "a moment was queued" alone would stamp the
// trailing `/quit` with a zero and print a placeholder for a terminator.

import (
	"testing"
	"time"

	"github.com/nick/gopi/internal/sessionevent"
)

// replayBandMoment walks the production seam for one line and returns the value
// the band would be handed: the driver's PUSH, the reader's POP into a lineRead,
// and THE PRODUCTION DECISION (chatTurnOpenedAt) — the function cmd/agent's turn
// loop itself calls, so this arm cannot pass on a copy of the rule.
//
// A RED PROOF IS THE REASON IT IS WRITTEN THIS WAY. The first version of this
// file re-implemented the `if stamped { use moment }` branch locally and stayed
// GREEN after the branch under test had been disabled in production — a test that
// agrees with itself proves nothing.
func replayBandMoment(t *testing.T, input *chatInput, recorded time.Time, push bool, liveNow time.Time) time.Time {
	t.Helper()
	if push {
		// THE DRIVER'S OWN CALL (chat_replay.go's typeTurn), immediately before
		// the keystrokes of that line.
		input.noteReplayMoment(recorded)
	}
	// THE READER'S POP — one per line received. The two values are what the turn
	// loop stores in its own lineRead (that struct is declared inside the loop),
	// and they are handed to the production decision verbatim.
	replayMoment, replayStamped := input.takeReplayMoment()
	// THE TURN LOOP'S DECISION, by calling the production function.
	return chatTurnOpenedAt(replayMoment, replayStamped, liveNow)
}

// TestReplayedTurnBandStatesTheRecordedMoment is the defect arm: a replay of an
// old line prints that line's own moment, NOT the moment it was replayed.
func TestReplayedTurnBandStatesTheRecordedMoment(t *testing.T) {
	recorded := time.Date(2026, 11, 7, 12, 11, 17, 0, time.FixedZone("CST", 8*3600))
	// The live arm's clock, i.e. "now" — a day later, and a different hour.
	liveNow := time.Date(2026, 11, 8, 15, 59, 32, 0, time.FixedZone("CST", 8*3600))

	got := replayBandMoment(t, &chatInput{}, recorded, true, liveNow)

	if got.Equal(liveNow) {
		t.Fatalf("the band was handed the CLOCK (%s) for a recorded turn — a replay of an old session claims today",
			sessionevent.FormatTurnStamp(got))
	}
	if want := sessionevent.FormatTurnStamp(recorded); sessionevent.FormatTurnStamp(got) != want {
		t.Fatalf("the band states %q, want the record's own moment %q",
			sessionevent.FormatTurnStamp(got), want)
	}
}

// TestReplayedTurnWithoutAMomentStatesThePlaceholder is the ABSENCE arm: the row
// recorded nothing, so the band must print the shared placeholder — not the
// clock, which would be an authoritative-looking wrong day.
func TestReplayedTurnWithoutAMomentStatesThePlaceholder(t *testing.T) {
	liveNow := time.Date(2026, 11, 8, 15, 59, 32, 0, time.FixedZone("CST", 8*3600))

	// push=true with the ZERO moment: the driver DID type this line, the record
	// simply holds no readable stamp for it.
	got := replayBandMoment(t, &chatInput{}, time.Time{}, true, liveNow)

	if !got.IsZero() {
		t.Fatalf("a momentless recorded turn was handed %s — the placeholder can never be reached, and the band would claim today",
			got.Format(time.RFC3339))
	}
	if stamp := sessionevent.FormatTurnStamp(got); stamp != sessionevent.TurnStampPlaceholder {
		t.Fatalf("the band states %q, want the shared placeholder %q", stamp, sessionevent.TurnStampPlaceholder)
	}
}

// TestLiveTurnBandKeepsTheClock is the COMPATIBILITY arm, and it is what stops
// the fix from becoming a regression: a line no replay driver queued (every line
// of an ordinary run, and a replay's trailing `/quit`) keeps the moment the REPL
// mints. Without this arm, "print the record's moment" could be satisfied by
// stamping every turn zero and the live surface would print placeholders forever.
func TestLiveTurnBandKeepsTheClock(t *testing.T) {
	liveNow := time.Date(2026, 11, 8, 15, 59, 32, 0, time.FixedZone("CST", 8*3600))

	got := replayBandMoment(t, &chatInput{}, time.Time{}, false, liveNow)

	if !got.Equal(liveNow) {
		t.Fatalf("a LIVE line was handed %s, want the minted moment %s", got.Format(time.RFC3339), liveNow.Format(time.RFC3339))
	}
}

// TestReplayMomentsPairInOrder pins the QUEUE property the whole pairing rests
// on: one push per turn typed, one pop per line received, in the same order. A
// queue that reordered — or that popped on the keystroke rather than on the read
// — would attach a turn's moment to a neighbour's band, which no single-turn test
// can see.
func TestReplayMomentsPairInOrder(t *testing.T) {
	input := &chatInput{}
	first := time.Date(2026, 11, 7, 12, 11, 17, 0, time.FixedZone("CST", 8*3600))
	second := time.Date(2026, 11, 8, 9, 3, 41, 0, time.FixedZone("CST", 8*3600))
	input.noteReplayMoment(first)
	input.noteReplayMoment(second)

	for i, want := range []time.Time{first, second} {
		got, stamped := input.takeReplayMoment()
		if !stamped {
			t.Fatalf("pop %d reported no moment although two were queued", i)
		}
		if !got.Equal(want) {
			t.Fatalf("pop %d = %s, want %s — the moments are not paired with their lines",
				i, got.Format(time.RFC3339), want.Format(time.RFC3339))
		}
	}
	// THE QUEUE IS EMPTY AFTER THE TURNS, which is the state the trailing `/quit`
	// reads: it must report "no moment queued" so the terminator keeps the live
	// arm rather than printing a record's placeholder.
	if at, stamped := input.takeReplayMoment(); stamped || !at.IsZero() {
		t.Fatalf("an exhausted queue reported stamped=%v moment=%s, want false/zero", stamped, at.Format(time.RFC3339))
	}
}
