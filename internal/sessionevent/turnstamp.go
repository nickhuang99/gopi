package sessionevent

// turnstamp.go — THE ONE SPELLING of the moment a turn OPENED, for every face
// that shows a turn banner to a person.
//
// THE THREE TIERS, and why only one of them lives here. The agent's own sense of
// time is already delivered in two higher tiers, both of which ride the WIRE and
// therefore must never move once written (prefix caching: one changed byte at
// the head of a request re-prefills everything after it):
//
//	B) the per-request injected line `[system] [t+~5m] @ 2026-11-08T12:27:01+08:00`
//	   (internal/agent/clock.go's TurnStamp) — the model's clock;
//	C) the session's own absolute start date, recorded once in session_start.
//
// This file is tier A: the one a PERSON reads, on the turn banner. It is
// RENDER-ONLY. It never enters a request, it never enters a session row, and it
// is derived from a value that is already on disk — the timestamp of the
// turn-opening row itself — so a replay of yesterday's session prints yesterday's
// moment and not the moment somebody happened to open the file.
//
// WHY THE VALUE IS THE ROW'S AND NOT THE CLOCK'S. Handing the banner `time.Now()`
// would look identical on a live screen and be WRONG in exactly the case a banner
// is worth having: reopening a conversation from last week, or replaying a
// recording. The record already states when each turn opened; a surface that
// asks the clock is inventing a second answer to a question that has one.

import "time"

const (
	// TurnStampLayout is the banner's clock spelling: a date, a space, and a
	// second-precision clock. It is deliberately NOT RFC3339 — that is tier B's
	// machine-readable form on the wire, and a person reading a banner does not
	// want `T` and an offset they cannot use. Seconds, not nanoseconds: a turn
	// banner is a landmark, not a measurement, and the trailing fraction is
	// six more cells of noise on a 60-column terminal.
	TurnStampLayout = "2006-01-02 15:04:05"

	// TurnStampPlaceholder is what a banner prints when the turn-opening row
	// carried no usable timestamp (a hand-edited record, a row written by a
	// writer older than the timestamp field, a truncated line).
	//
	// IT IS NOT A FALLBACK TO NOW. Substituting the current clock here would
	// produce a banner that looks authoritative and states the wrong day — the
	// exact defect the historical-value rule exists to prevent. An explicit
	// absence is the honest answer, and it is what the tests pin (Rule 15).
	TurnStampPlaceholder = "--:--:--"

	// TurnStampSep is the separator between a banner's identity ("turn 7") and
	// its moment. It is the same `·` the turn label already uses between the
	// ordinal and the prompt, so a banner reads as one line of one vocabulary
	// rather than two joined notations.
	TurnStampSep = " · "
)

// FormatTurnStamp renders one turn's opening moment for a banner, or the
// explicit placeholder when there is no moment to render.
//
// THE ZERO TIME IS THE ONLY "UNKNOWN", and it is stated rather than replaced.
// A caller that has a real time gets it in ITS OWN ZONE: a timestamp parsed out
// of a session record carries the offset that record was written with, so this
// function neither normalises to UTC (which would print a different wall clock
// than the one the session ran on) nor reaches for time.Local (which would print
// the reader's zone and move a historical moment every time the file travels).
//
// It is ONE function because two faces call it — the terminal band header
// (internal/lineedit) and the web turn banner (cmd/agent) — and two spellings of
// "when did this turn open" is a drift nobody would notice until the two screens
// disagreed about the same turn.
func FormatTurnStamp(at time.Time) string {
	if at.IsZero() {
		return TurnStampPlaceholder
	}
	return at.Format(TurnStampLayout)
}
