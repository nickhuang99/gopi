package agent

// selfrecord_retention_test.go — the FIRST note of a session survives the
// session.
//
// The defect this pins (operator's ruling, 2026-11-08): `maxSelfRecordNotes` was
// 50 and the append path silently sliced the OLDEST notes off. A long session
// therefore lost the beginning of its own record — the same "your context was
// quietly reduced" failure the compaction epic removes, reappearing one layer
// down. Retention is now a CEILING that nothing realistic reaches (2,000 notes;
// the largest record measured on this host holds 805), so the assertion is not
// "fewer were dropped" but "note #1 is still there, verbatim, after 60+ rounds".

import (
	"io"
	"log"
	"strings"
	"testing"

	"github.com/nick/gopi/internal/config"
)

// TestSelfRecordKeepsTheFirstNoteForTheWholeSession records one note, then 79
// more (comfortably past the old 50-note cut), and asserts the FIRST one is
// still held AND still reaches the model through the re-injection carrier.
func TestSelfRecordKeepsTheFirstNoteForTheWholeSession(t *testing.T) {
	t.Setenv(config.GopiDirEnv, t.TempDir())
	ag := &Agent{Logger: log.New(io.Discard, "", 0), Ctx: &Context{}}

	const firstNote = "the very first finding of this session — must not be truncated"
	record := func(note string) {
		t.Helper()
		content := selfRecordOpenMarker + "\n" + note + "\n" + selfRecordCloseMarker
		if got := ag.recordSelfNotes(content, ""); len(got) != 1 {
			t.Fatalf("recordSelfNotes returned %d note(s) for %q, want 1", len(got), note)
		}
	}

	record(firstNote)
	// 79 further rounds: past the retired 50-note cut, well inside the ceiling.
	for i := 0; i < 79; i++ {
		record("later round finding " + itoaSelfRecord(i))
	}

	held := ag.selfRecord.notes
	if len(held) != 80 {
		t.Fatalf("expected all 80 notes retained, got %d", len(held))
	}
	if held[0] != firstNote {
		t.Fatalf("the FIRST note was truncated: want %q, got %q", firstNote, held[0])
	}

	// Retention that does not reach the model is not retention: the carrier the
	// compaction path re-injects must still carry note #1.
	if !strings.Contains(ag.selfRecordCarrier(), firstNote) {
		t.Fatalf("the re-injection carrier dropped the first note:\n%s", ag.selfRecordCarrier())
	}
}

// TestSelfRecordCeilingIsNotARetentionPolicy pins the CEILING's purpose: it is
// a stop on pathological growth, not a retention window. It must be far above
// the largest record measured on this host (805 notes) — otherwise the epic's
// "your notes are kept" would be a claim the code makes false on a real session.
func TestSelfRecordCeilingIsNotARetentionPolicy(t *testing.T) {
	const largestMeasuredRecordNotes = 805 // ~/.gopi/sessions/*/self-record.md, this host 2026-11-08
	if maxSelfRecordNotes <= largestMeasuredRecordNotes {
		t.Fatalf("ceiling %d does not clear the largest measured record (%d notes): the cap would truncate a real session's beginning", maxSelfRecordNotes, largestMeasuredRecordNotes)
	}
	// Worst case per note is maxSelfRecordNoteChars; the ceiling must not be so
	// large that the carrier can never be reasoned about at all.
	if maxSelfRecordNotes*maxSelfRecordNoteChars < 2*largestMeasuredRecordNotes*maxSelfRecordNoteChars {
		t.Fatalf("ceiling %d is under 2x the largest measured record (%d) — no margin for a longer session", maxSelfRecordNotes, largestMeasuredRecordNotes)
	}
}
