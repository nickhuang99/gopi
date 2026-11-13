package rundir

// THE MULTI-SEGMENT RECORD (epic: the silent zero).
//
// A log that `gopi session resume` continued carries a SECOND `session_start`
// header and a SECOND `session_end` verdict, appended in that order. The tail
// read is last-writer-wins, so the record's Turns/Tokens/ElapsedSec were the
// LAST segment's own numbers — and the resumed segment routinely reports 0
// passes, which internal/tools' run line then dropped through `if r.Turns > 0`:
// no count, no dash, no reason.
//
// These tests pin the FACTS the record must carry instead of a guess:
//
//	EndSegments    how many session_end verdicts the read saw
//	StartSegments  how many session_start headers it saw (a resume's proof)
//	TailTruncated  whether the read began past byte 0
//	ScaleTrusted   whether the terminal block describes the RUN
//
// Measured shape (2026-10-27, session 4a0957a3-bb4e-4337-a761-20606c87d1fd,
// 641754 bytes): two headers, two verdicts, total_iterations [29, 0].

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	// segFirstTurns / segLastTurns are the measured pair: the killed run's own
	// count, then the resumed segment's 0.
	segFirstTurns = 29
	segLastTurns  = 0
	// segStart is the fixture's session_start timestamp.
	segStart = "2026-10-27T08:01:32.02276172+08:00"
)

// twoSegmentLog writes a two-segment run log under dir and returns its path.
func twoSegmentLog(t *testing.T, dir, runID string) string {
	t.Helper()
	header := `{"type":"session_start","sessionId":"` + runID + `","timestamp":"` + segStart + `"}`
	resumed := `{"type":"session_start","sessionId":"` + runID + `","timestamp":"` + segStart +
		`","resumedFrom":"` + runID + `"}`
	body := header + "\n" +
		`{"type":"request","iteration":1}` + "\n" +
		`{"type":"session_end","elapsed_sec":4549.425534492,"exit_code":1,"total_iterations":` +
		strconv.Itoa(segFirstTurns) + `,"total_tokens":24714762,"timestamp":"` + segStart + `"}` + "\n" +
		resumed + "\n" +
		`{"type":"session_end","elapsed_sec":0,"exit_code":0,"total_iterations":` +
		strconv.Itoa(segLastTurns) + `,"total_tokens":0,"timestamp":"` + segStart + `"}` + "\n"
	path := filepath.Join(dir, runID+".jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

// TestRecordCountsBothSegmentsOfAResumedLog is THE fixture the epic names: two
// `session_end` events (29 then 0) in one log.
func TestRecordCountsBothSegmentsOfAResumedLog(t *testing.T) {
	dir := t.TempDir()
	twoSegmentLog(t, dir, "run-two-seg")
	rec := RecordsDir(dir)[0]

	if !rec.HasEnd {
		t.Fatalf("HasEnd = false — the log ends with a session_end")
	}
	if rec.EndSegments != 2 {
		t.Errorf("EndSegments = %d, want 2 — the read saw both verdicts and must say so", rec.EndSegments)
	}
	if rec.StartSegments != 2 {
		t.Errorf("StartSegments = %d, want 2 — the resume's own header is the boundary proof", rec.StartSegments)
	}
	if rec.TailTruncated {
		t.Errorf("TailTruncated = true — the fixture is far smaller than the window")
	}
	// Turns keeps the field's documented meaning (the LAST session_end's own
	// count); what changes is that the record SAYS that segment is the last.
	if rec.Turns != segLastTurns {
		t.Errorf("Turns = %d, want the last segment's own count %d", rec.Turns, segLastTurns)
	}
	// And the number is NOT presented as the run's total.
	if rec.ScaleTrusted {
		t.Errorf("ScaleTrusted = true for a log holding %d segments — a last-writer-wins number is not a run total",
			rec.EndSegments)
	}
}

// TestRecordTrustsASingleSegmentLog is the control: the ordinary shape must
// still be trusted, or the fix would turn every run line into a dash.
func TestRecordTrustsASingleSegmentLog(t *testing.T) {
	dir := t.TempDir()
	writeJSONL(t, filepath.Join(dir, "run-one.jsonl"),
		`{"type":"session_start","sessionId":"run-one","timestamp":"`+segStart+`"}`,
		`{"type":"session_end","elapsed_sec":12.5,"exit_code":0,"total_iterations":42,"total_tokens":99}`)
	rec := RecordsDir(dir)[0]
	if rec.EndSegments != 1 || rec.StartSegments != 1 {
		t.Errorf("segments = %d start / %d end, want 1/1", rec.StartSegments, rec.EndSegments)
	}
	if !rec.ScaleTrusted {
		t.Errorf("ScaleTrusted = false for a whole-file single-segment log — its verdict IS the run's total")
	}
}

// TestRecordStatesATruncatedRead is the honest-unknown half: when the window
// began past byte 0, a segment that ended before it is invisible, so the
// terminal block cannot be called the run's total even with one verdict in the
// window.
func TestRecordStatesATruncatedRead(t *testing.T) {
	dir := t.TempDir()
	// A log larger than the window whose ONLY verdict sits at the far end:
	// everything before it is filler, so the read cannot know how many
	// segments preceded.
	filler := strings.Repeat(`{"type":"request","iteration":1}`+"\n", int(runHintTailBytes/32)+64)
	body := `{"type":"session_start","sessionId":"run-big","timestamp":"` + segStart + `"}` + "\n" + filler +
		`{"type":"session_end","elapsed_sec":9,"exit_code":0,"total_iterations":42,"total_tokens":99}` + "\n"
	path := filepath.Join(dir, "run-big.jsonl")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	rec := RecordsDir(dir)[0]
	if !rec.TailTruncated {
		t.Fatalf("TailTruncated = false for a %d-byte log read through a %d-byte window",
			len(body), runHintTailBytes)
	}
	if rec.EndSegments != 1 {
		t.Errorf("EndSegments = %d, want 1 (one verdict inside the window)", rec.EndSegments)
	}
	if rec.ScaleTrusted {
		t.Errorf("ScaleTrusted = true for a TRUNCATED read — the segment before the window is invisible here")
	}
}
