package rundir

// rundir_segments_scan_test.go — the WHOLE-FILE segment stamp, proved on the
// SMALLEST sample (Rule 10.4): a three-segment log a few hundred bytes long, so
// a failure is visible by reading the fixture.
//
// The real session this fixes is 21398592 bytes with 18601354 bytes between its
// last `session_start` and the end of the file — a window measurement, not a
// fixture. The unit test below pins the SEMANTICS (last, not first; counted, not
// guessed; unscanned when unwalkable), which is what a later rewrite could
// silently break.

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const (
	segScanPID1 = 5100001
	segScanPID2 = 5100002
	segScanPID3 = 5100003
	// segScanLongSegmentBytes is how much filler a segment carries in the
	// "far from the end" case — enough that a bounded tail window could not
	// reach the headers, which is the shape of the real file.
	segScanLongSegmentBytes = 1 << 16
)

// writeSegmentedLog writes a run log holding one start/end pair per pid, with
// each segment's verdict one hour after its header. fillerBytes, when non-zero,
// is written between the header and the verdict of EVERY segment (that is what
// pushes the headers away from the tail).
func writeSegmentedLog(t *testing.T, r pidsSegments, fillerBytes int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "session.jsonl")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create %s: %v", path, err)
	}
	defer f.Close()
	base := time.Date(2026, 10, 28, 1, 0, 0, 0, time.UTC)
	filler := make([]byte, fillerBytes)
	for i := range filler {
		filler[i] = 'x'
	}
	for i, pid := range r.PIDs {
		start := base.Add(time.Duration(i) * 24 * time.Hour)
		end := start.Add(time.Hour)
		if _, err := f.WriteString(`{"type":"session_start","sessionId":"seg-scan","pid":` +
			strconv.Itoa(pid) + `,"timestamp":"` + start.Format(time.RFC3339Nano) + `"}` + "\n"); err != nil {
			t.Fatalf("write header: %v", err)
		}
		if fillerBytes > 0 {
			if _, err := f.Write(filler); err != nil {
				t.Fatalf("write filler: %v", err)
			}
			if _, err := f.WriteString("\n"); err != nil {
				t.Fatalf("write filler newline: %v", err)
			}
		}
		if _, err := f.WriteString(`{"type":"session_end","exit_code":0,"reason":"interrupt","elapsed_sec":3600,"timestamp":"` +
			end.Format(time.RFC3339Nano) + `"}` + "\n"); err != nil {
			t.Fatalf("write verdict: %v", err)
		}
	}
	return path
}

// pidsSegments names the segment pids a fixture log carries, in order.
type pidsSegments struct{ PIDs []int }

// TestScanSegmentsReportsTheLastSegment — the epic's unit-level red-proof. It
// must report the THIRD pid and the THIRD verdict's instant, count three
// headers and three verdicts, and call the log resumed.
func TestScanSegmentsReportsTheLastSegment(t *testing.T) {
	path := writeSegmentedLog(t, pidsSegments{PIDs: []int{segScanPID1, segScanPID2, segScanPID3}}, 0)
	stamp, ok := ScanSegments(path)
	if !ok {
		t.Fatalf("ScanSegments(%s) reported ok=false on a readable log", path)
	}
	if stamp.Starts != 3 || stamp.Ends != 3 {
		t.Fatalf("counts = %d starts / %d ends, want 3/3", stamp.Starts, stamp.Ends)
	}
	if stamp.LastStartPID != segScanPID3 {
		t.Errorf("LastStartPID = %d, want %d (the LAST header's)", stamp.LastStartPID, segScanPID3)
	}
	if !stamp.Resumed() {
		t.Error("a three-segment log does not report as resumed")
	}
	wantEnd := time.Date(2026, 10, 28, 2, 0, 0, 0, time.UTC).Add(48 * time.Hour)
	if !stamp.HasLastEnd || !stamp.LastEndTime.Equal(wantEnd) {
		t.Errorf("LastEndTime = %v (has=%v), want %v", stamp.LastEndTime, stamp.HasLastEnd, wantEnd)
	}
	if stamp.LastEndReason != "interrupt" {
		t.Errorf("LastEndReason = %q, want %q", stamp.LastEndReason, "interrupt")
	}
	if !stamp.HasLastEndExit || stamp.LastEndExit != 0 {
		t.Errorf("exit = (%d, has=%v), want (0, true) — 0 is a real value (Rule 15a)",
			stamp.LastEndExit, stamp.HasLastEndExit)
	}
}

// TestScanSegmentsReadsPastAWholeSegmentOfFiller is the shape of the REAL file:
// the headers sit megabytes from the end, so nothing but a whole-file walk
// reaches the last one. The filler makes a bounded read miss it by construction.
func TestScanSegmentsReadsPastAWholeSegmentOfFiller(t *testing.T) {
	path := writeSegmentedLog(t, pidsSegments{PIDs: []int{segScanPID1, segScanPID2, segScanPID3}}, segScanLongSegmentBytes)
	stamp, ok := ScanSegments(path)
	if !ok {
		t.Fatalf("ScanSegments(%s) reported ok=false", path)
	}
	if stamp.Starts != 3 || stamp.LastStartPID != segScanPID3 {
		t.Errorf("starts=%d lastPID=%d, want 3 and %d", stamp.Starts, stamp.LastStartPID, segScanPID3)
	}
}

// TestScanSegmentsOnASingleSegmentSession — the ordinary case must read as
// NOT resumed; the disclosure that fires on every session would say nothing.
func TestScanSegmentsOnASingleSegmentSession(t *testing.T) {
	path := writeSegmentedLog(t, pidsSegments{PIDs: []int{segScanPID1}}, 0)
	stamp, ok := ScanSegments(path)
	if !ok {
		t.Fatal("ScanSegments reported ok=false on a readable log")
	}
	if stamp.Starts != 1 || stamp.Ends != 1 {
		t.Fatalf("counts = %d/%d, want 1/1", stamp.Starts, stamp.Ends)
	}
	if stamp.Resumed() {
		t.Error("a single-segment log reports as resumed")
	}
	if stamp.LastStartPID != segScanPID1 {
		t.Errorf("LastStartPID = %d, want %d", stamp.LastStartPID, segScanPID1)
	}
}

// TestScanSegmentsRefusesAnAbsentLog — an unreadable file must be ok=false so
// the caller keeps its bounded answer and every derived field reports what was
// actually read, rather than an empty stamp passing as "one segment" (Rule 15).
func TestScanSegmentsRefusesAnAbsentLog(t *testing.T) {
	if _, ok := ScanSegments(filepath.Join(t.TempDir(), "nope.jsonl")); ok {
		t.Error("ScanSegments reported ok=true for a path that does not exist")
	}
	if _, ok := ScanSegments(""); ok {
		t.Error("ScanSegments reported ok=true for the empty path")
	}
}

// TestShowRecordCarriesTheSegmentStamp — the one-row read is the ONLY place the
// stamp is attached, and it must leave the listing's bounded answer alone.
func TestShowRecordCarriesTheSegmentStamp(t *testing.T) {
	path := writeSegmentedLog(t, pidsSegments{PIDs: []int{segScanPID1, segScanPID2, segScanPID3}}, segScanLongSegmentBytes)
	rec := ShowRecord(path)
	if !rec.SegmentsScanned {
		t.Fatal("ShowRecord did not scan the log")
	}
	if rec.Segments.Starts != 3 {
		t.Errorf("Segments.Starts = %d, want 3", rec.Segments.Starts)
	}
	if got := rec.LastWriterPID(); got != segScanPID3 {
		t.Errorf("LastWriterPID = %d, want %d (the LAST segment's)", got, segScanPID3)
	}
	if _, ok := rec.LastEndTime(); !ok {
		t.Error("LastEndTime reported no recorded ending on a log that carries three")
	}

	// The LISTING read stays bounded: HeadRecord does not know the segment
	// count, and must not claim it does.
	head := HeadRecord(path)
	if head.SegmentsScanned {
		t.Error("HeadRecord scanned the whole log — the listing's bounded read is the point")
	}
	if got := head.LastWriterPID(); got != segScanPID1 {
		t.Errorf("HeadRecord.LastWriterPID = %d, want the header pid %d", got, segScanPID1)
	}
}

// TestScanSegmentsSumsTheRecordedTotals — the stamp's End* fields are the
// session's RECORDED cost, and the criterion is the one
// internal/gopilog's reader already established (task 01a17ae2): each
// `session_end` reports only ITS OWN run segment, so the session's figure is
// the SUM over segments, never the last verdict's.
//
// The shape that makes a last-wins reading wrong is written here as data: the
// second segment ran NO turn (`total_iterations: 0`) because the resume
// delivered its prompt to a run that ended before reaching one. Last-wins
// would report 0 passes and 0 tokens for a two-segment session — a measurement
// that erases the work the FIRST segment recorded.
func TestScanSegmentsSumsTheRecordedTotals(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "resumed.jsonl")
	body := "" +
		`{"type":"session_start","sessionId":"sum","pid":1,"timestamp":"2026-11-04T08:00:00Z"}` + "\n" +
		`{"type":"session_end","exit_code":0,"reason":"complete","elapsed_sec":120.5,"total_iterations":7,"total_tokens":1000,"timestamp":"2026-11-04T08:02:00Z"}` + "\n" +
		`{"type":"session_start","sessionId":"sum","pid":2,"resumed_from":"s1","timestamp":"2026-11-04T09:00:00Z"}` + "\n" +
		`{"type":"session_end","exit_code":0,"reason":"complete","elapsed_sec":30.0,"total_iterations":0,"total_tokens":0,"timestamp":"2026-11-04T09:00:30Z"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	stamp, ok := ScanSegments(path)
	if !ok {
		t.Fatal("ScanSegments reported ok=false on a readable log")
	}
	if !stamp.HasEndTotals {
		t.Fatal("HasEndTotals = false on a log carrying two verdicts")
	}
	if stamp.EndPasses != 7 {
		t.Errorf("EndPasses = %d, want 7 — the segments are SUMMED, and a last-wins reading reports the empty segment's 0", stamp.EndPasses)
	}
	if stamp.EndTokens != 1000 {
		t.Errorf("EndTokens = %d, want 1000", stamp.EndTokens)
	}
	if stamp.EndElapsedSec != 150.5 {
		t.Errorf("EndElapsedSec = %v, want 150.5", stamp.EndElapsedSec)
	}
	// The stamp must NOT break the last-segment facts it already reported:
	// LastEndReason/HasLastEndExit still describe the LAST verdict alone.
	if stamp.LastEndReason != "complete" {
		t.Errorf("LastEndReason = %q, want %q", stamp.LastEndReason, "complete")
	}
}

// TestScanSegmentsReportsNoTotalsWithoutAVerdict — a live run (or one killed
// before its end hook) wrote no `session_end`. HasEndTotals must be false so
// every renderer states the absence instead of printing a 0 it never measured
// (Rule 15).
func TestScanSegmentsReportsNoTotalsWithoutAVerdict(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "live.jsonl")
	body := `{"type":"session_start","sessionId":"live","pid":9,"timestamp":"2026-11-04T08:00:00Z"}` + "\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	stamp, ok := ScanSegments(path)
	if !ok {
		t.Fatal("ScanSegments reported ok=false on a readable log")
	}
	if stamp.HasEndTotals {
		t.Error("HasEndTotals = true on a log with no session_end — an absence must not read as a measurement")
	}
	if stamp.EndPasses != 0 || stamp.EndTokens != 0 || stamp.EndElapsedSec != 0 {
		t.Errorf("totals = (%d, %d, %v), want zero values with HasEndTotals false",
			stamp.EndPasses, stamp.EndTokens, stamp.EndElapsedSec)
	}
}
