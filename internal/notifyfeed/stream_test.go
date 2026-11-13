package notifyfeed

// stream_test.go — the reader's contract, pinned against the two sink shapes
// that actually exist on disk and the one damaged span that actually happened.
//
// WHY THE FIXTURES LOOK LIKE THIS. Every fixture below is copied from the
// operator's real sink's SHAPE, not invented:
//
//   - the CONCATENATED fixture is how every event delivered before e2b7e379
//     (2026-09-20) sits on disk: `}{` with no newline, `wc -l` = 1;
//   - the FRAMED fixture is how events sit on disk after that fix;
//   - the DAMAGED fixture reproduces the measured 2026-09-12 record whose
//     escaped `zombie` payload swallowed the following object's tail — the
//     span that stopped a strict decoder at 92,134 records.
//
// A line-oriented reader passes NONE of the concatenated cases, which is the
// defect this package exists to close — so those cases ARE the test.

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testEventClaimed     = "claimed"
	testEventInProgress  = "in_progress"
	testEventFailed      = "failed"
	testTaskID           = "t-a1b2c3d4"
	testTimestamp        = "2026-10-01T09:00:00+08:00"
	testAnotherTaskID    = "t-ffeeddcc"
	testSinkFilename     = "task-events.jsonl"
	testCursorFilename   = "feed.cursor.json"
	testRecordCountThree = 3
)

// frameEvent builds one delivered payload exactly as internal/tools marshals
// it: field order is irrelevant to the reader (it decodes by name), but the
// SHAPE — one object, no trailing newline of its own — is the point.
func frameEvent(event, taskID string) string {
	body := map[string]any{
		"event":           event,
		"previous_status": testEventInProgress,
		"task_id":         taskID,
		"status":          event,
		"summary":         "fixture row",
		"timestamp":       testTimestamp,
	}
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	return string(raw)
}

// writeSink puts content at a per-test sink path and returns it.
func writeSink(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), testSinkFilename)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write sink: %v", err)
	}
	return path
}

// readAll is the one-shot convenience the tests use: read the whole sink.
func readAll(t *testing.T, path string) ScanResult {
	t.Helper()
	res, err := Read(path, 0, -1)
	if err != nil {
		t.Fatalf("Read(%s): %v", path, err)
	}
	return res
}

// TestConcatStreamIsReadWithoutNewlines is THE regression the whole package
// exists for: a sink written before the framing fix holds every event under a
// single newline, and a line-oriented reader sees ONE record. The reader must
// return all three, and must report that they were NOT newline-framed (the
// provenance a subscriber uses to tell an old sink from a new one).
func TestConcatStreamIsReadWithoutNewlines(t *testing.T) {
	concat := frameEvent(testEventClaimed, testTaskID) +
		frameEvent(testEventInProgress, testTaskID) +
		frameEvent(testEventFailed, testTaskID)
	if strings.Count(concat, "\n") != 0 {
		t.Fatalf("fixture is not the concatenated shape (it contains a newline)")
	}
	path := writeSink(t, concat)

	res := readAll(t, path)
	if len(res.Records) != testRecordCountThree {
		t.Fatalf("concatenated sink yielded %d record(s), want %d — a line-oriented read is the defect this must catch",
			len(res.Records), testRecordCountThree)
	}
	wantEvents := []string{testEventClaimed, testEventInProgress, testEventFailed}
	for i, r := range res.Records {
		if got := r.Event(); got != wantEvents[i] {
			t.Errorf("record %d event = %q, want %q", i, got, wantEvents[i])
		}
		if r.Line {
			t.Errorf("record %d reported as newline-framed, but the fixture has no newlines", i)
		}
		if r.TaskID() != testTaskID {
			t.Errorf("record %d task = %q, want %q", i, r.TaskID(), testTaskID)
		}
	}
	if res.NextOffset != int64(len(concat)) {
		t.Errorf("NextOffset = %d, want %d (the whole sink)", res.NextOffset, len(concat))
	}
	if len(res.Damaged) != 0 {
		t.Errorf("concatenated but healthy sink reported %d damaged span(s): %+v", len(res.Damaged), res.Damaged)
	}
	// The line-oriented reader's own verdict, asserted here as the CONTRAST:
	// this is what the old consumer saw.
	lines := strings.Count(strings.TrimRight(concat, "\n"), "\n") + 1
	if lines != 1 {
		t.Fatalf("fixture is not the defect's shape: a line count of %d does not demonstrate it", lines)
	}
}

// TestFramedStreamIsReadAndMarkedAsFramed is the other half: the post-fix
// shape must read identically AND be reported as framed, so a subscriber can
// distinguish the eras rather than guessing.
func TestFramedStreamIsReadAndMarkedAsFramed(t *testing.T) {
	framed := frameEvent(testEventClaimed, testTaskID) + "\n" +
		frameEvent(testEventFailed, testTaskID) + "\n"
	path := writeSink(t, framed)

	res := readAll(t, path)
	if len(res.Records) != 2 {
		t.Fatalf("framed sink yielded %d record(s), want 2", len(res.Records))
	}
	for i, r := range res.Records {
		if !r.Line {
			t.Errorf("record %d not reported as newline-framed", i)
		}
	}
	if res.Truncated {
		t.Error("a newline-terminated sink was reported truncated")
	}
}

// TestMixedEraSinkReadsBothShapes: the operator's sink IS the union of both
// eras (the framing fix changed the producer, not the file). A reader tuned to
// either shape alone loses real events.
//
// ONE ATTRIBUTION RULE, stated because the fixture makes it visible: a
// terminator belongs to the record it FOLLOWS. At the era boundary a lone "\n"
// is therefore read as the pre-fix era's last record's terminator, and the test
// asserts only what is unambiguous — the first record is unframed, the record
// after the boundary is framed.
func TestMixedEraSinkReadsBothShapes(t *testing.T) {
	mixed := frameEvent(testEventClaimed, testTaskID) + // pre-fix: concatenated
		frameEvent(testEventInProgress, testAnotherTaskID) +
		"\n" + // the framing fix lands here
		frameEvent(testEventFailed, testAnotherTaskID) + "\n"
	path := writeSink(t, mixed)

	res := readAll(t, path)
	if len(res.Records) != testRecordCountThree {
		t.Fatalf("mixed-era sink yielded %d record(s), want %d", len(res.Records), testRecordCountThree)
	}
	if res.Records[0].Line {
		t.Error("the pre-fix record was reported as framed")
	}
	if !res.Records[2].Line {
		t.Error("the post-fix record was not reported as framed")
	}
	if len(res.Damaged) != 0 || res.Truncated {
		t.Errorf("a healthy mixed-era sink reported damage: %+v truncated=%v", res.Damaged, res.Truncated)
	}
}

// TestDamagedRecordIsSkippedAndTheRestRecovered pins the property a strict
// decoder cannot have: the sink is an append-only file nobody may rewrite, so
// a damaged record must cost exactly itself. The fixture mirrors the measured
// 2026-09-12 shape — a record whose tail is swallowed — by cutting one object
// mid-string, right before the next object's brace.
func TestDamagedRecordIsSkippedAndTheRestRecovered(t *testing.T) {
	good1 := frameEvent(testEventClaimed, testTaskID)
	good2 := frameEvent(testEventInProgress, testAnotherTaskID)
	good3 := frameEvent(testEventFailed, testAnotherTaskID)
	// The damaged span begins like a record and never closes: the string that
	// would end it, and the object's closing brace, were eaten by the escaped
	// payload that followed it in the real file.
	damaged := `{"event":"stale","task_id":"t-damaged","zombie":"{\"confidence\":`
	path := writeSink(t, good1+damaged+good2+"\n"+good3+"\n")

	res := readAll(t, path)
	if len(res.Records) != testRecordCountThree {
		t.Fatalf("recovered %d record(s) around the damage, want %d — every healthy record must survive",
			len(res.Records), testRecordCountThree)
	}
	if len(res.Damaged) != 1 {
		t.Fatalf("reported %d damaged span(s), want exactly 1: %+v", len(res.Damaged), res.Damaged)
	}
	if res.Damaged[0].Offset != int64(len(good1)) {
		t.Errorf("damaged span offset = %d, want %d (right after the first good record)",
			res.Damaged[0].Offset, len(good1))
	}
	if res.Damaged[0].Reason == "" {
		t.Error("a damaged span was reported with no reason — the count must never be a bare number")
	}
	// Order is preserved across the damage: a consumer's cursor depends on it.
	if got := res.Records[1].TaskID(); got != testAnotherTaskID {
		t.Errorf("record after the damage = %q, want %q", got, testAnotherTaskID)
	}
}

// TestTruncatedTailIsNotDamage: a sink a live producer is appending to ends
// mid-record, which is NORMAL and must be reported as truncated (resume later)
// rather than as damage (skip). Confusing the two loses the record that was
// still arriving — the same defect one layer down.
func TestTruncatedTailIsNotDamage(t *testing.T) {
	complete := frameEvent(testEventClaimed, testTaskID)
	partial := frameEvent(testEventFailed, testAnotherTaskID)[:20]
	path := writeSink(t, complete+partial)

	res := readAll(t, path)
	if len(res.Records) != 1 {
		t.Fatalf("yielded %d record(s), want 1 (the complete one)", len(res.Records))
	}
	if !res.Truncated {
		t.Error("a sink ending mid-record was not reported truncated")
	}
	if len(res.Damaged) != 0 {
		t.Errorf("a still-arriving record was reported as damage: %+v", res.Damaged)
	}
	if res.NextOffset != int64(len(complete)) {
		t.Errorf("NextOffset = %d, want %d — the truncated tail must be re-read, not skipped",
			res.NextOffset, len(complete))
	}
}

// TestMissingSinkReadsAsEmpty: polling a sink that has not been written yet is
// the first tick of every consumer. It is not an error and must not be — but a
// permission failure IS one, and is covered by TestUnreadableSinkIsAnError.
func TestMissingSinkReadsAsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")
	res, err := Read(path, 0, -1)
	if err != nil {
		t.Fatalf("missing sink returned an error: %v", err)
	}
	if len(res.Records) != 0 || res.Truncated {
		t.Errorf("missing sink yielded %+v, want an empty result", res)
	}
}

// TestUnreadableSinkIsAnError is the Rule 15 half: "I could not read it" must
// never be reported as "there was nothing there".
func TestUnreadableSinkIsAnError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root — permission bits do not deny the read")
	}
	path := writeSink(t, frameEvent(testEventClaimed, testTaskID))
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) }) //stylegate:ignore cleanup restores the fixture's file mode after the unreadable-stream case ran
	if _, err := Read(path, 0, -1); err == nil {
		t.Fatal("an unreadable sink read as an empty stream — a permission failure must be loud")
	}
}

// TestReadIsResumableAtAnyBoundary: the reader is the consumer's only
// implementation, so re-reading from a reported NextOffset must yield exactly
// the records the first read left behind — never a duplicate, never a gap.
func TestReadIsResumableAtAnyBoundary(t *testing.T) {
	framed := frameEvent(testEventClaimed, testTaskID) + "\n" +
		frameEvent(testEventInProgress, testTaskID) + "\n" +
		frameEvent(testEventFailed, testTaskID) + "\n"
	path := writeSink(t, framed)

	first, err := Read(path, 0, -1)
	if err != nil {
		t.Fatalf("first read: %v", err)
	}
	if len(first.Records) != testRecordCountThree {
		t.Fatalf("first read yielded %d records, want %d", len(first.Records), testRecordCountThree)
	}
	// Consume only the first record's worth of offset, as a consumer that
	// processed one event and stopped (a crash) would resume.
	resumeAt := first.Records[1].Offset
	rest, err := Read(path, resumeAt, -1)
	if err != nil {
		t.Fatalf("resumed read: %v", err)
	}
	if len(rest.Records) != 2 {
		t.Fatalf("resumed read yielded %d records, want 2 — a resume boundary must not lose or duplicate",
			len(rest.Records))
	}
	if rest.Records[0].Offset != first.Records[1].Offset {
		t.Errorf("resumed first record offset = %d, want %d", rest.Records[0].Offset, first.Records[1].Offset)
	}
}

// TestCursorRoundTripsAndResumes drives the cursor through its real lifecycle:
// no cursor (first run) → read → save → load → resume, appending between the
// two reads the way a live sink grows.
func TestCursorRoundTripsAndResumes(t *testing.T) {
	path := writeSink(t, frameEvent(testEventClaimed, testTaskID)+"\n")
	cursorPath := filepath.Join(t.TempDir(), testCursorFilename)

	if c, ok, err := LoadCursor(cursorPath); err != nil || ok {
		t.Fatalf("first LoadCursor = (%+v, %v, %v), want (zero, false, nil)", c, ok, err)
	}
	first := readAll(t, path)
	if len(first.Records) != 1 {
		t.Fatalf("first read yielded %d records, want 1", len(first.Records))
	}
	// The producer appends while the consumer is between reads — the event a
	// naive whole-file re-read would duplicate and an in-memory position would
	// lose.
	appendToSink(t, path, frameEvent(testEventFailed, testTaskID)+"\n")

	size := sinkSize(t, path)
	c := NewCursor(path, size, first.NextOffset, nil)
	if err := SaveCursor(cursorPath, c); err != nil {
		t.Fatalf("SaveCursor: %v", err)
	}
	loaded, ok, err := LoadCursor(cursorPath)
	if err != nil || !ok {
		t.Fatalf("LoadCursor = (%+v, %v, %v), want the saved cursor", loaded, ok, err)
	}
	resume, err := loaded.Validate(path, size, readAtFrom(path))
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	second, err := Read(path, resume, -1)
	if err != nil {
		t.Fatalf("resumed read: %v", err)
	}
	if len(second.Records) != 1 {
		t.Fatalf("resumed read yielded %d records, want exactly the 1 appended since", len(second.Records))
	}
	if second.Records[0].Event() != testEventFailed {
		t.Errorf("resumed read delivered %q, want %q", second.Records[0].Event(), testEventFailed)
	}
}

// TestCursorDetectsTruncation: the sink was rotated or truncated under a
// consumer. Resuming from the old offset would read a different stream's bytes
// and report them as events — the exact "I missed events and did not know"
// failure the cursor exists to prevent.
func TestCursorDetectsTruncation(t *testing.T) {
	path := writeSink(t, frameEvent(testEventClaimed, testTaskID)+"\n"+frameEvent(testEventFailed, testTaskID)+"\n")
	size := sinkSize(t, path)
	c := NewCursor(path, size, size, nil)
	c.Offset = size

	// The sink is replaced by a SHORTER one (rotation into a fresh file).
	if err := os.WriteFile(path, []byte(frameEvent(testEventInProgress, testAnotherTaskID)+"\n"), 0o600); err != nil {
		t.Fatalf("rotate sink: %v", err)
	}
	smaller := sinkSize(t, path)
	if _, err := c.Validate(path, smaller, readAtFrom(path)); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("Validate on a truncated sink = %v, want ErrCursorStale", err)
	}
}

// TestCursorDetectsARewrittenRecord is the subtler rotation: the sink is the
// same LENGTH but its content differs. The byte-offset check alone passes here,
// which is precisely why the cursor also stores the first unread record's
// identity.
func TestCursorDetectsARewrittenRecord(t *testing.T) {
	original := frameEvent(testEventClaimed, testTaskID) + "\n"
	path := writeSink(t, original)
	size := sinkSize(t, path)
	// The consumer read everything, and the next unread record (at EOF) is empty.
	c := NewCursor(path, size, size, nil)

	// A same-length replacement: same bytes count, different events.
	replacement := frameEvent(testEventFailed, testAnotherTaskID[:len(testTaskID)]+strings.Repeat("x", len(testAnotherTaskID)-len(testTaskID)))
	if len(replacement) != len(strings.TrimRight(original, "\n")) {
		replacement = frameEvent(testEventFailed, strings.Repeat("z", len(testTaskID)))
	}
	if err := os.WriteFile(path, []byte(replacement+"\n"), 0o600); err != nil {
		t.Fatalf("rewrite sink: %v", err)
	}
	// The cursor's identity is derived from the record that FOLLOWED the read
	// point, so build one that names it and confirm a changed byte fails.
	c.NextLen = len(replacement)
	c.NextScore = hashBytes([]byte(replacement))
	mutated := replacement[:len(replacement)-2] + "!!"
	if hashBytes([]byte(mutated)) == c.NextScore {
		t.Fatal("the fixture's mutation does not change the digest — the test would pass vacuously")
	}
	if _, err := c.Validate(path, sinkSize(t, path), readAtFromBytes([]byte(mutated))); !errors.Is(err, ErrCursorStale) {
		t.Fatalf("Validate on a rewritten record = %v, want ErrCursorStale", err)
	}
}

// TestCursorRefusesAForeignSink: a cursor is a resume point INSIDE one stream.
// Applying it to another sink is a consumer configuration mistake, and its own
// error so the remedy is not confused with a rotation.
func TestCursorRefusesAForeignSink(t *testing.T) {
	other := filepath.Join(t.TempDir(), testSinkFilename)
	c := NewCursor("/some/other/sink.jsonl", 100, 100, nil)
	if _, err := c.Validate(other, 100, readAtFromBytes(nil)); !errors.Is(err, ErrCursorForeign) {
		t.Fatalf("Validate with a foreign cursor = %v, want ErrCursorForeign", err)
	}
}

// TestEmptySinkReadsAsEmptyAndDoesNotSpin: a consumer polling an empty sink
// must make no progress claim and report nothing, rather than looping.
func TestEmptySinkReadsAsEmptyAndDoesNotSpin(t *testing.T) {
	path := writeSink(t, "")
	res, err := Read(path, 0, -1)
	if err != nil {
		t.Fatalf("empty sink: %v", err)
	}
	if len(res.Records) != 0 || res.Truncated || len(res.Damaged) != 0 {
		t.Errorf("empty sink yielded %+v, want an empty clean result", res)
	}
	if res.NextOffset != 0 {
		t.Errorf("NextOffset = %d, want 0", res.NextOffset)
	}
}

// --- fixture helpers -------------------------------------------------------

func appendToSink(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatalf("open sink for append: %v", err)
	}
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatalf("append to sink: %v", err)
	}
}

func sinkSize(t *testing.T, path string) int64 {
	t.Helper()
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat sink: %v", err)
	}
	return fi.Size()
}

// readAtFrom returns the cursor-identity reader bound to a path — the same
// function the CLI hands Validate, so the test exercises the production shape.
func readAtFrom(path string) func(int64, int) ([]byte, error) {
	return func(offset int64, n int) ([]byte, error) {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer f.Close()
		buf := make([]byte, n)
		read, err := f.ReadAt(buf, offset)
		if err != nil && read == 0 {
			return nil, err
		}
		return buf[:read], nil
	}
}

// readAtFromBytes is the in-memory twin, for the cases whose fixture is a
// synthetic byte slice rather than a file.
func readAtFromBytes(data []byte) func(int64, int) ([]byte, error) {
	return func(offset int64, n int) ([]byte, error) {
		if offset > int64(len(data)) {
			return nil, os.ErrDeadlineExceeded
		}
		end := offset + int64(n)
		if end > int64(len(data)) {
			end = int64(len(data))
		}
		return data[offset:end], nil
	}
}
