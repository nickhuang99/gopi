package sessionevent

// turnstamp_test.go — the ONE spelling of a turn's opening moment, pinned.
//
// These arms are deliberately small and deliberately three, because the value of
// a shared formatter is not its cleverness but the three properties two faces
// depend on: a REAL moment renders in the banner's own layout, an ABSENT moment
// renders as the explicit placeholder (never the current clock — that is the
// defect the whole function exists to prevent), and a moment keeps THE ZONE IT
// WAS WRITTEN IN (a session that ran in +08:00 must not print a UTC wall clock
// when the record is read somewhere else).

import (
	"strings"
	"testing"
	"time"
)

// TestFormatTurnStampRendersTheRecordedMomentInItsOwnZone is the positive arm:
// the string the terminal band and the web banner both show.
func TestFormatTurnStampRendersTheRecordedMomentInItsOwnZone(t *testing.T) {
	at := time.Date(2026, 11, 8, 12, 31, 20, 987654321, time.FixedZone("CST", 8*3600))
	got := FormatTurnStamp(at)
	want := "2026-11-08 12:31:20"
	if got != want {
		t.Fatalf("FormatTurnStamp(%s) = %q, want %q", at, got, want)
	}
	// THE ZONE IS THE RECORD'S, NOT THE READER'S. Re-rendering the same instant
	// in UTC is a different wall clock, and a banner that printed it would state
	// a time the session never ran at.
	if utc := at.UTC().Format(TurnStampLayout); utc == got {
		t.Fatalf("the fixture is zone-degenerate: %q renders identically in UTC", got)
	}
	if strings.Contains(got, "T") || strings.Contains(got, "+") {
		t.Fatalf("FormatTurnStamp returned a wire timestamp, not a banner: %q", got)
	}
}

// TestFormatTurnStampStatesAnAbsentMomentIsThePlaceholder — Rule 15's arm. A row
// with no timestamp is an ABSENCE, and the honest rendering of an absence is not
// "now": a banner that reached for the clock here would look authoritative and
// state the wrong day, which is exactly what a replayed session would show.
func TestFormatTurnStampStatesAnAbsentMomentIsThePlaceholder(t *testing.T) {
	for _, zero := range []time.Time{{}, time.Time{}.UTC()} {
		if got := FormatTurnStamp(zero); got != TurnStampPlaceholder {
			t.Fatalf("FormatTurnStamp(%v) = %q, want the explicit placeholder %q", zero, got, TurnStampPlaceholder)
		}
	}
	// The placeholder must not be parseable as a real moment of today, or a
	// reader could not tell it from one.
	if now := time.Now().Format(TurnStampLayout); TurnStampPlaceholder == now {
		t.Fatalf("the placeholder collides with the current clock (%q) — an absence would read as a measurement", now)
	}
}

// TestTurnStampVocabularyIsTheLabelsOwn keeps the separator shared with the turn
// label's own field separator: a banner is one line of one vocabulary, not two
// notations joined.
func TestTurnStampVocabularyIsTheLabelsOwn(t *testing.T) {
	if TurnStampSep != " · " {
		t.Fatalf("TurnStampSep = %q, want the `·` separator the turn label already uses", TurnStampSep)
	}
	if strings.Contains(TurnStampLayout, "T15") {
		t.Fatalf("TurnStampLayout %q is the wire layout; a banner must not carry `T`", TurnStampLayout)
	}
}
